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

		// wait for meta cluster
		sysnc, err := connectSystem(ctx)
		Expect(err).ToNot(HaveOccurred())
		Eventually(metaClusterReady(sysnc), "20s", "1s").Should(BeTrue())
	})

	AfterEach(func() { cancel() })

	Describe("Create and Relocate", Ordered, func() {
		Describe("Create", Ordered, func() {
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
					nfo, err := stream.Information()
					if err != nil {
						return false
					}

					return nfo.Cluster.Name == "c1"
				}, "10s", "1s").Should(BeTrue())

				Expect(publishToStream(nc, "js.in.RELOCATE", 101, 200)).To(Succeed())
				Expect(streamMessagesAndSequences(stream, 200)).Should(Succeed())
			})
		})

		Describe("Validate", func() {
			It("Should exist and have messages", func() {
				stream, err := mgr.LoadStream("RELOCATE")
				Expect(err).ToNot(HaveOccurred())

				nfo, _ := stream.LatestInformation()
				Expect(nfo.Cluster.Name).To(Equal("c1"))
				Expect(streamMessagesAndSequences(stream, 200)).Should(Succeed())

				Expect(publishToStream(nc, "js.in.RELOCATE", 201, 300)).To(Succeed())
				Expect(streamMessagesAndSequences(stream, 300)).Should(Succeed())
			})
		})
	})
})
