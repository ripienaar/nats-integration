package streams

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nats-io/jsm.go"
	"github.com/nats-io/nats.go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Tests stream relocation and scaling:
//
// Create:
// - creates RELOCATE in C2
// - puts 100 messages in
// - moves it to c1, check 100 messages
// - puts 100 messages in
//
// Validate:
// - checks it's in c1 and has 200 messages
// - adds 100 messages and checks
var _ = Describe("Stream Relocation", Ordered, func() {
	var (
		ctx    context.Context
		cancel context.CancelFunc
		nc     *nats.Conn
		mgr    *jsm.Manager
		err    error
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), time.Minute)
		defer cancel()

		nc, mgr, err = connectUser(ctx)
		Expect(err).ToNot(HaveOccurred())

		if strings.HasPrefix(nc.ConnectedServerVersion(), "2.7") {
			Skip(fmt.Sprintf("Test cannot be run on server %s", nc.ConnectedServerVersion()))
		}

		// wait for meta cluster
		sysnc, err := connectSystem(ctx)
		Expect(err).ToNot(HaveOccurred())
		Eventually(metaClusterReady(sysnc), "20s", "1s").Should(BeTrue())
	})

	AfterEach(func() { cancel() })

	Describe("Create and Relocate", Ordered, func() {
		Describe("Create", func() {
			It("Should create and publish message into c2", func() {
				if os.Getenv("VALIDATE_ONLY") != "" {
					Skip("Validating only")
				}

				Expect(mgr.IsKnownStream("RELOCATE")).To(BeFalse())
				stream, err := mgr.NewStream("RELOCATE",
					jsm.Subjects("js.in.RELOCATE"),
					jsm.Replicas(3),
					jsm.PlacementCluster("c2"),
					jsm.FileStorage())
				Expect(err).ToNot(HaveOccurred())

				nfo, err := stream.Information()
				Expect(err).ToNot(HaveOccurred())
				Expect(nfo.Cluster.Name).To(Equal("c2"))
				Expect(nfo.Cluster.Leader).ToNot(BeEmpty())
				Expect(nfo.Cluster.Replicas).To(HaveLen(2))

				Expect(publishToStream(nc, "js.in.RELOCATE", 1, 100)).To(Succeed())
				Expect(streamMessagesAndSequences(stream, 100)).Should(Succeed())
			})

			It("Should relocate to the other cluster", func() {
				if os.Getenv("VALIDATE_ONLY") != "" {
					Skip("Validating only")
				}

				stream, err := mgr.LoadStream("RELOCATE")
				Expect(err).ToNot(HaveOccurred())

				err = stream.UpdateConfiguration(stream.Configuration(), jsm.PlacementCluster("c1"))
				Expect(err).ToNot(HaveOccurred())

				Eventually(func() bool {
					nfo, _ := stream.Information()
					return nfo != nil && nfo.Cluster.Name == "c1"
				}, "10s", "1s").Should(BeTrue(), "Waiting for stream to relocate to c1")

				// Wait for data move
				Eventually(func() error {
					return streamMessagesAndSequences(stream, 100)
				}, "10s", "1s").Should(Succeed(), "Waiting for stream to come online with 100 messages in new cluster")

				Expect(publishToStream(nc, stream.Subjects()[0], 101, 100)).To(Succeed())
				Expect(streamMessagesAndSequences(stream, 200)).Should(Succeed(), "Stream size after publishing to relocated stream")
			})
		})

		Describe("Validate", Ordered, func() {
			It("Should exist and have messages", func() {
				stream, err := mgr.LoadStream("RELOCATE")
				Expect(err).ToNot(HaveOccurred())

				Eventually(streamLeader(stream), "10s", "1s").Should(BeTrue())

				nfo, _ := stream.LatestInformation()
				msgs := int(nfo.State.Msgs)

				Expect(msgs).To(BeNumerically(">=", 200))
				Expect(nfo.Cluster.Name).To(Equal("c1"))
				Expect(nfo.Cluster.Replicas).To(HaveLen(2))

				Expect(streamMessagesAndSequences(stream, msgs)).Should(Succeed())

				Expect(publishToStream(nc, "js.in.RELOCATE", msgs+1, 100)).To(Succeed())
				Expect(streamMessagesAndSequences(stream, msgs+100)).Should(Succeed())
			})
		})
	})
})
