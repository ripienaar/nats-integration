package streams

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/nats-io/jsm.go"
	"github.com/nats-io/jsm.go/api"
	"github.com/nats-io/nats.go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

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

				for i := 0; i < 100; i++ {
					_, err := nc.Request(stream.Subjects()[0], []byte(fmt.Sprintf("Message %d", i)), time.Second)
					Expect(err).ToNot(HaveOccurred())
				}

				Eventually(streamMessages(stream), "10s").Should(Equal(100))
			})
		})

		Describe("Validate", func() {
			It("Should exist and have messages", func() {
				stream, err := mgr.LoadStream("BASIC")
				Expect(err).ToNot(HaveOccurred())

				nfo, err := stream.LatestInformation()
				Expect(err).ToNot(HaveOccurred())

				Expect(nfo.State.Msgs).To(Equal(uint64(100)))
				Expect(nfo.Cluster.Name).To(Equal("c2"))
			})
		})
	})

	Describe("Mirror in other cluster", func() {
		Describe("Create", func() {
			It("Should create and publish message into c2", func() {
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

				nfo, _ := stream.LatestInformation()
				Expect(nfo.State.Msgs).To(Equal(uint64(100)))
				Expect(nfo.Cluster.Name).To(Equal("c1"))
			})
		})
	})
})
