package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"sync"
	"time"

	"github.com/klauspost/compress/s2"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

func exitIfErr(err error, format string, a ...interface{}) {
	if err == nil {
		return
	}

	fmt.Printf(format, a...)
	os.Exit(1)
}

type jszr struct {
	Data   server.JSInfo     `json:"data"`
	Server server.ServerInfo `json:"server"`
}

func main() {
	nc, err := nats.Connect("localhost:10000", nats.UserInfo("system", "password"))
	exitIfErr(err, "Connection failed: %v", err)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	for {
		if ctx.Err() != nil {
			log.Printf("Timeout\n")
			os.Exit(1)
		}

		res, err := doReq(ctx, map[string]interface{}{"leader_only": true}, "$SYS.REQ.SERVER.PING.JSZ", 1, nc)
		if err != nil {
			log.Printf("Request failed: %v", err)
			time.Sleep(time.Second)
			continue
		}

		if len(res) == 0 {
			log.Printf("Did not receive a response from the meta leader")
			time.Sleep(time.Second)
			continue
		}

		response := jszr{}

		err = json.Unmarshal(res[0], &response)
		if err != nil {
			log.Printf("Invalid response: %v", err)
			time.Sleep(1)
			continue
		}

		if response.Data.Meta == nil {
			log.Printf("Did not receive meta info")
			time.Sleep(1)
			continue
		}

		meta := response.Data.Meta

		if len(meta.Replicas) != 5 {
			log.Printf("Waiting for 6 replicas, got %d", len(meta.Replicas)+1)
			time.Sleep(time.Second)
			continue
		}

		current := true
		for _, p := range meta.Replicas {
			if !p.Current {
				log.Printf("Peer %s is not current", p.Name)
				current = false
			}
		}
		if !current {
			time.Sleep(time.Second)
			continue
		}

		log.Printf("Got %d meta cluster members", len(meta.Replicas))
		return
	}
}

func doReqAsync(ctx context.Context, req interface{}, subj string, waitFor int, nc *nats.Conn, cb func([]byte)) error {
	jreq := []byte("{}")
	var err error

	if req != nil {
		jreq, err = json.MarshalIndent(req, "", "  ")
		if err != nil {
			return err
		}
	}

	var (
		mu  sync.Mutex
		ctr = 0
	)

	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	var finisher *time.Timer
	if waitFor == 0 {
		finisher = time.NewTimer(300 * time.Millisecond)
		go func() {
			select {
			case <-finisher.C:
				cancel()
			case <-ctx.Done():
				return
			}
		}()
	}

	errs := make(chan error)
	sub, err := nc.Subscribe(nc.NewRespInbox(), func(m *nats.Msg) {
		mu.Lock()
		defer mu.Unlock()

		data := m.Data
		if m.Header.Get("Content-Encoding") == "snappy" {
			ud, err := ioutil.ReadAll(s2.NewReader(bytes.NewBuffer(data)))
			if err != nil {
				errs <- err
				return
			}
			data = ud
		}

		if finisher != nil {
			finisher.Reset(300 * time.Millisecond)
		}

		if m.Header.Get("Status") == "503" {
			errs <- nats.ErrNoResponders
			return
		}

		cb(data)
		ctr++

		if waitFor > 0 && ctr == waitFor {
			cancel()
		}
	})
	if err != nil {
		return err
	}
	defer sub.Unsubscribe()

	if waitFor > 0 {
		sub.AutoUnsubscribe(waitFor)
	}

	msg := nats.NewMsg(subj)
	msg.Data = jreq
	if subj != "$SYS.REQ.SERVER.PING" {
		msg.Header.Set("Accept-Encoding", "snappy")
	}
	msg.Reply = sub.Subject

	err = nc.PublishMsg(msg)
	if err != nil {
		return err
	}

	select {
	case err = <-errs:
		if err == nats.ErrNoResponders {
			return fmt.Errorf("server request failed, ensure the account used has system privileges and appropriate permissions")
		}
		return err
	case <-ctx.Done():
	}

	return err
}

func doReq(ctx context.Context, req interface{}, subj string, waitFor int, nc *nats.Conn) ([][]byte, error) {
	res := [][]byte{}
	mu := sync.Mutex{}

	err := doReqAsync(ctx, req, subj, waitFor, nc, func(r []byte) {
		mu.Lock()
		res = append(res, r)
		mu.Unlock()
	})

	return res, err
}
