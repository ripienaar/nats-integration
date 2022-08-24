package streams

import (
	"context"
	"os"
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
		nc  *nats.Conn
		mgr *jsm.Manager
		err error
	)

	BeforeEach(func() {
		if os.Getenv("SKIP_RELOCATION") != "" {
			Skip("Skipping relocation tests due to environment override")
		}

		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()

		nc, mgr, err = connectUser(ctx)
		Expect(err).ToNot(HaveOccurred())

		// wait for meta cluster
		sysnc, err := connectSystem(ctx)
		Expect(err).ToNot(HaveOccurred())
		Eventually(metaClusterReady(sysnc), "20s", "1s").Should(BeTrue())
	})

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

				Eventually(func() bool {
					nfo, _ := stream.Information()
					return nfo != nil && len(nfo.Cluster.Replicas) == 2
				}, "10s", "1s").Should(BeTrue(), "Waiting for stream to relocate to c1")

				nfo, err := stream.Information()
				Expect(err).ToNot(HaveOccurred())
				msgs := int(nfo.State.Msgs)

				Expect(msgs).To(BeNumerically(">=", 200))
				Expect(nfo.Config.Replicas).To(Equal(3))
				Expect(nfo.Cluster.Name).To(Equal("c1"))
				Expect(nfo.Cluster.Replicas).To(HaveLen(2))

				Expect(streamMessagesAndSequences(stream, msgs)).Should(Succeed())

				Expect(publishToStream(nc, "js.in.RELOCATE", msgs+1, 100)).To(Succeed())
				Expect(streamMessagesAndSequences(stream, msgs+100)).Should(Succeed())
			})
		})
	})
})
