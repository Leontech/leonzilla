// Small SFTP server for local testing: go run ./tools/sftptestserver <port> <root> [user] [password]
// default user "test", password "test"; listens on 127.0.0.1 only
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	"log"
	"net"
	"os"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func main() {
	if len(os.Args) < 3 {
		log.Fatal("usage: sftptestserver <port> <root> [user] [password]")
	}
	port, root := os.Args[1], os.Args[2]
	user, pass := "test", "test"
	if len(os.Args) > 4 {
		user, pass = os.Args[3], os.Args[4]
	}
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(priv)
	cfg := &ssh.ServerConfig{PasswordCallback: func(c ssh.ConnMetadata, p []byte) (*ssh.Permissions, error) {
		if c.User() == user && string(p) == pass {
			return nil, nil
		}
		return nil, fmt.Errorf("denied")
	}}
	cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		log.Fatal(err)
	}
	for {
		nc, err := ln.Accept()
		if err != nil {
			continue
		}
		go func() {
			_, chans, reqs, err := ssh.NewServerConn(nc, cfg)
			if err != nil {
				return
			}
			go ssh.DiscardRequests(reqs)
			for nch := range chans {
				if nch.ChannelType() != "session" {
					nch.Reject(ssh.UnknownChannelType, "")
					continue
				}
				ch, in, _ := nch.Accept()
				go func() {
					for req := range in {
						ok := req.Type == "subsystem" && string(req.Payload[4:]) == "sftp"
						req.Reply(ok, nil)
						if ok {
							srv, _ := sftp.NewServer(ch)
							if err := srv.Serve(); err != nil && err != io.EOF {
								log.Print(err)
							}
							ch.Close()
						}
					}
				}()
			}
		}()
	}
}
