// Copyright (c) 2016 Dominik Zeromski <dzeromsk@gmail.com>
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package main

import (
	"flag"
	"io"
	"io/ioutil"
	"log"
	"net"
	"sync"

	"golang.org/x/crypto/ssh"
)

var (
	listen  = flag.String("listen", ":2022", "local listen addr")
	keypath = flag.String("key", "./id_rsa", "path to server rsa key")

	remote = flag.String("remote", ":2023", "remote server addr")
	user   = flag.String("user", "testuser", "remote server username")
	pass   = flag.String("pass", "testpass", "remote server password")

	serverConfig *ssh.ServerConfig
	clientConfig *ssh.ClientConfig
)

func main() {
	clientConfig = &ssh.ClientConfig{
		User: *user,
		Auth: []ssh.AuthMethod{ssh.Password(*pass)},
	}

	serverConfig = &ssh.ServerConfig{
		NoClientAuth: true,
	}

	file, err := ioutil.ReadFile(*keypath)
	if err != nil {
		log.Fatal(err)
	}

	key, err := ssh.ParsePrivateKey(file)
	if err != nil {
		log.Fatal(err)
	}

	serverConfig.AddHostKey(key)

	server, err := net.Listen("tcp4", *listen)
	if err != nil {
		log.Fatal(err)
	}

	for {
		conn, err := server.Accept()
		if err != nil {
			log.Fatal(err)
		}

		go handleConnection(conn)
	}
}

func handleConnection(conn net.Conn) {
	defer conn.Close()

	sshConn, chans, reqs, err := ssh.NewServerConn(conn, serverConfig)
	if err != nil {
		log.Print(err)
		return
	}
	defer sshConn.Close()

	// ignore global requests
	go ssh.DiscardRequests(reqs)

	for ch := range chans {
		if ch.ChannelType() != "session" {
			ch.Reject(ssh.UnknownChannelType, ch.ChannelType())
			return
		}

		session, requests, err := ch.Accept()
		if err != nil {
			log.Print(err)
			continue
		}

		// TODO(dzeromsk): support multiple channels in one connection?
		// if yes remember to close connection
		// go handleSession(session, requests)
		handleSession(session, requests)
	}
}

func handleSession(serverChannel ssh.Channel, serverRequests <-chan *ssh.Request) {
	defer serverChannel.Close()

	clientChannel, clientRequests, err := newSession("tcp4", *remote, clientConfig)
	if err != nil {
		log.Print(err)
		return
	}
	defer clientChannel.Close()

	go channelForward(serverChannel, clientChannel)

	for {
		select {
		case msg := <-serverRequests:
			sendRequest(clientChannel, msg)
		case msg := <-clientRequests:
			sendRequest(serverChannel, msg)

		}
	}
}

func newSession(network, addr string, config *ssh.ClientConfig) (ssh.Channel, <-chan *ssh.Request, error) {
	client, err := ssh.Dial(network, addr, config)
	if err != nil {
		return nil, nil, err
	}

	ch, req, err := client.OpenChannel("session", nil)
	if err != nil {
		return nil, nil, err
	}
	return ch, req, nil
}

func channelForward(a ssh.Channel, b ssh.Channel) {
	var once sync.Once
	close := func() {
		a.Close()
		b.Close()
	}

	go func() {
		io.Copy(a, b)
		once.Do(close)
	}()

	io.Copy(b, a)
	once.Do(close)
}

func sendRequest(ch ssh.Channel, msg *ssh.Request) {
	if msg == nil {
		return
	}

	ok, err := ch.SendRequest(msg.Type, msg.WantReply, msg.Payload)
	if err != nil {
		log.Print(err)

	}
	if msg.WantReply {
		msg.Reply(ok, nil)
	}
}
