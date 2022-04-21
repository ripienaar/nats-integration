package streams

import (
	"context"
	"os"
	"time"

	"github.com/nats-io/jsm.go"
	"github.com/nats-io/jsm.go/api"
	"github.com/nats-io/nats.go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Tests a stream and it's mirror:
//
// Create:
// - Sets up BASIC stream in c2 cluster
// - Put 100 messages into it
// - Create a mirror BASIC_MIRROR in c1 cluster
// - Waits for sync to complete
//
// Verify:
// - Checks it's in the right cluster
// - Checks 100 messages
// - Adds 100 messages to BASIC and wait on MIRROR for them
// - Verifies
var _ = Describe("Basic Stream with Mirrors", Ordered, func() {
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

	Describe("Basic Stream With Placement", func() {
		Describe("Create", func() {
			It("Should create and publish message into c2", func() {
				if os.Getenv("VALIDATE_ONLY") != "" {
					Skip("Validating only")
				}

				Expect(mgr.IsKnownStream("BASIC")).To(BeFalse())

				stream, err := mgr.NewStream("BASIC",
					jsm.Subjects("js.in.BASIC"),
					jsm.Replicas(3),
					jsm.PlacementCluster("c2"),
					jsm.FileStorage())
				Expect(err).ToNot(HaveOccurred())

				Expect(publishToStream(nc, "js.in.BASIC", 1, 100)).To(Succeed())
				Expect(streamMessagesAndSequences(stream, 100)).Should(Succeed())
			})
		})

		Describe("Validate", func() {
			It("Should exist and have messages", func() {
				stream, err := mgr.LoadStream("BASIC")
				Expect(err).ToNot(HaveOccurred())

				nfo, _ := stream.LatestInformation()
				Expect(nfo.Cluster.Name).To(Equal("c2"))

				Expect(streamMessagesAndSequences(stream, 100)).Should(Succeed())
			})
		})
	})

	Describe("Mirror in other cluster", func() {
		Describe("Create", func() {
			It("Should create and publish message into c1", func() {
				if os.Getenv("VALIDATE_ONLY") != "" {
					Skip("Validating only")
				}

				Expect(mgr.IsKnownStream("BASIC")).To(BeTrue())
				Expect(mgr.IsKnownStream("BASIC_MIRROR")).To(BeFalse())

				stream, err := mgr.NewStream("BASIC_MIRROR",
					jsm.Replicas(3),
					jsm.PlacementCluster("c1"),
					jsm.FileStorage(),
					jsm.Mirror(&api.StreamSource{Name: "BASIC"}))
				Expect(err).ToNot(HaveOccurred())

				// wait for the mirror to sync
				Eventually(streamMessages(stream), "10s").Should(Equal(100))
			})
		})

		Describe("Validate", func() {
			It("Should exist and have messages", func() {
				stream, err := mgr.LoadStream("BASIC_MIRROR")
				Expect(err).ToNot(HaveOccurred())

				Expect(streamMessagesAndSequences(stream, 100)).Should(Succeed())

				Expect(publishToStream(nc, "js.in.BASIC", 101, 200)).To(Succeed())
				Eventually(streamMessages(stream), "10s").Should(Equal(200))
				Expect(streamMessagesAndSequences(stream, 200)).Should(Succeed())

				nfo, _ := stream.LatestInformation()
				Expect(nfo.Cluster.Name).To(Equal("c1"))
			})
		})
	})
})