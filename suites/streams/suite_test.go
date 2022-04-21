package streams

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"testing"
	"time"

	"github.com/nats-io/jsm.go"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestStreams(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Streams Suite")
}

func connectUser(ctx context.Context) (*nats.Conn, *jsm.Manager, error) {
	for {
		nc, err := nats.Connect("nats://one:password@localhost:10000")
		if err == nil {
			mgr, _ := jsm.New(nc)
			return nc, mgr, nil
		}

		err = ctxSleep(ctx, time.Second)
		if err != nil {
			return nil, nil, err
		}
	}
}

func connectSystem(ctx context.Context) (*nats.Conn, error) {
	for {
		nc, err := nats.Connect("nats://system:password@localhost:10000")
		if err == nil {
			return nc, nil
		}

		err = ctxSleep(ctx, time.Second)
		if err != nil {
			return nil, err
		}
	}
}

func ctxSleep(ctx context.Context, d time.Duration) error {
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// determines if stream has a leader, for use with Eventually()
func streamLeader(s *jsm.Stream) func() bool {
	return func() bool {
		nfo, _ := s.Information()
		return nfo != nil && nfo.Cluster.Leader != ""
	}
}

// gets stream messages, for use with Eventually()
func streamMessages(s *jsm.Stream) func() int {
	return func() int {
		nfo, err := s.State()
		if err != nil {
			return 0
		}

		return int(nfo.Msgs)
	}
}

// waits for the meta cluster to be ready, for use with Eventually()
func metaClusterReady(nc *nats.Conn) func() bool {
	return func() bool {
		type jszr struct {
			Data   server.JSInfo     `json:"data"`
			Server server.ServerInfo `json:"server"`
		}

		res, err := nc.Request("$SYS.REQ.SERVER.PING.JSZ", []byte(`{"leader_only":true}`), 2*time.Second)
		if err != nil {
			log.Printf("JSZ Request failed on %s: %v", nc.ConnectedServerName(), err)
			return false
		}

		response := jszr{}

		err = json.Unmarshal(res.Data, &response)
		if err != nil {
			log.Printf("JSZ Request failed: %v", err)
			return false
		}

		if response.Data.Meta == nil {
			log.Printf("No cluster metadata")
			return false
		}

		meta := response.Data.Meta

		if len(meta.Replicas) != 5 {
			log.Printf("Got %d replicas", len(meta.Replicas)+1)
			return false
		}

		current := true
		for _, p := range meta.Replicas {
			if !p.Current {
				log.Printf("%s not current", p.Name)
				current = false
			}
		}
		if !current {
			return false
		}

		return true
	}
}

func publishToStream(nc *nats.Conn, subj string, start int, count int) error {
	var err error

	for i := 0; i < count; i++ {
		for try := 0; try < 5; try++ {
			_, err = nc.Request(subj, []byte(fmt.Sprintf("%d", start+i)), time.Second)
			if err == nil {
				break
			}

			log.Printf("Could not publish message %d on try %d/5 to %s: %v", i, try, subj, err)
			time.Sleep(time.Second)
		}

		if err != nil {
			return err
		}
	}

	return nil
}

func streamMessagesAndSequences(stream *jsm.Stream, count int) error {
	nfo, err := stream.Information()
	if err != nil {
		return fmt.Errorf("stream info failed: %v", err)
	}

	if nfo.State.Msgs != uint64(count) {
		return fmt.Errorf("found %d messages", nfo.State.Msgs)
	}

	if nfo.State.FirstSeq != 1 && nfo.State.LastSeq != uint64(count)-1 {
		return fmt.Errorf("sequences wrong first:%d last:%d", nfo.State.FirstSeq, nfo.State.LastSeq)
	}

	for i := 1; i <= count; i++ {
		msg, err := stream.ReadMessage(uint64(i))
		if err != nil {
			return err
		}
		if string(msg.Data) != fmt.Sprintf("%d", i) {
			return fmt.Errorf("message %d had body %q", msg.Sequence, msg.Data)
		}
	}
	return nil
}
