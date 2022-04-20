package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/nats-io/jsm.go"
	"github.com/nats-io/jsm.go/api"
	"github.com/nats-io/nats.go"
)

func exitIfErr(err error, format string, a ...interface{}) {
	if err == nil {
		return
	}

	fmt.Printf(format, a...)
	os.Exit(1)
}

func handleStreamFile(mgr *jsm.Manager, f string) error {
	log.Printf("Processing Stream %s", f)

	jcfg, err := os.ReadFile(f)
	if err != nil {
		return err
	}

	cfg := &api.StreamConfig{}

	err = json.Unmarshal(jcfg, cfg)
	if err != nil {
		return err
	}

	known, err := mgr.IsKnownStream(cfg.Name)
	if err != nil {
		return err
	}
	if known {
		return fmt.Errorf("stream %s already existed", cfg.Name)
	}

	stream, err := mgr.NewStreamFromDefault(cfg.Name, *cfg)
	if err != nil {
		return err
	}

	if len(cfg.Subjects) > 0 {
		for i := 0; i <= 1000; i++ {
			_, err := mgr.NatsConn().Request(cfg.Subjects[0], []byte(fmt.Sprintf("Message %d", i)), time.Second)
			if err != nil {
				return err
			}
		}

		nfo, err := stream.Information()
		if err != nil {
			return err
		}
		if nfo.State.Msgs != 1000 {
			return fmt.Errorf("stream did not have 1000 messages: %d", nfo.State.Msgs)
		}
	}
	return nil
}

func main() {
	nc, err := nats.Connect("localhost:10000", nats.UserInfo("one", "password"))
	exitIfErr(err, "Connection failed: %v", err)

	mgr, err := jsm.New(nc)
	exitIfErr(err, "JSM failed: %v", err)

	failed := false

	files := []string{}
	err = filepath.Walk("../streams", func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if filepath.Ext(info.Name()) == ".json" {
			files = append(files, filepath.Join("../streams", info.Name()))
		}

		return nil
	})
	exitIfErr(err, "Creating streams failed: %v", err)

	sort.Strings(files)

	for _, file := range files {
		err := handleStreamFile(mgr, file)
		if err != nil {
			log.Printf("Handling %s failed: %v\n", file, err)
			failed = true
		}
	}

	if failed {
		exitIfErr(errors.New(""), "Stream creation failed")
	}
}
