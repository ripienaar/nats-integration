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

// Create:
// - Creates a R1, scales up to R3
// - Publish messages to it and verify health
// - Creates a R3, scales down to R1
// - Publish messages to it and verify health
//
// Verify:
// - Loads the streams and verify they are healthy at new R level
// - Publish messages and verify
var _ = Describe("Scale Up and Down Replicas", Ordered, func() {
	var (
		nc  *nats.Conn
		mgr *jsm.Manager
		err error
	)

	BeforeEach(func() {
		if os.Getenv("SKIP_REPLICAS") != "" {
			Skip("Skipping replica scale up/down tests due to environment override")
		}

		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()

		nc, mgr, err = connectUser(ctx)
		Expect(err).ToNot(HaveOccurred())

		// wait for meta cluster
		sysnc, err := connectSystem(ctx)
		Expect(err).ToNot(HaveOccurred())
		Eventually(metaClusterReady(sysnc), "30s", "1s").Should(BeTrue())
	})

	Describe("Basic scale down", Ordered, func() {
		Describe("Create", func() {
			BeforeEach(func() {
				if os.Getenv("VALIDATE_ONLY") != "" {
					Skip("Validating only")
				}
			})

			It("Should create and publish messages", func() {
				Expect(mgr.IsKnownStream("BASIC_SCALE_DOWN")).To(BeFalse())

				stream, err := mgr.NewStream("BASIC_SCALE_DOWN",
					jsm.Subjects("js.in.BASIC_SCALE_DOWN"),
					jsm.Replicas(3),
					jsm.PlacementCluster("c2"),
					jsm.FileStorage())
				Expect(err).ToNot(HaveOccurred())

				Expect(publishToStream(nc, "js.in.BASIC_SCALE_DOWN", 1, 100)).To(Succeed())
				Expect(streamMessagesAndSequences(stream, 100)).Should(Succeed())
			})

			It("Should scale down to R1", func() {
				stream, err := mgr.LoadStream("BASIC_SCALE_DOWN")
				Expect(err).ToNot(HaveOccurred())

				err = stream.UpdateConfiguration(stream.Configuration(), jsm.Replicas(1))
				Expect(err).ToNot(HaveOccurred())

				Eventually(streamLeader(stream), "10s", "1s").Should(BeTrue())
				Eventually(streamClusterReady(stream, 1), "10s", "1s").Should(Succeed())
			})
		})

		Describe("Validate", func() {
			It("Should exist and have messages", func() {
				stream, err := mgr.LoadStream("BASIC_SCALE_DOWN")
				Expect(err).ToNot(HaveOccurred())

				Eventually(streamClusterReady(stream, 1), "10s", "1s").Should(Succeed())

				nfo, _ := stream.LatestInformation()
				msgs := nfo.State.Msgs
				Expect(msgs).To(BeNumerically(">=", 100))

				Expect(nfo.Config.Replicas).To(Equal(1))
				Expect(nfo.Cluster.Replicas).To(HaveLen(0))

				Expect(publishToStream(nc, "js.in.BASIC_SCALE_DOWN", int(msgs+1), 100)).To(Succeed())
				Expect(streamMessagesAndSequences(stream, int(msgs+100))).Should(Succeed())
			})
		})
	})

	Describe("Basic scale up", Ordered, func() {
		Describe("Create", func() {
			BeforeEach(func() {
				if os.Getenv("VALIDATE_ONLY") != "" {
					Skip("Validating only")
				}
			})

			It("Should create and publish messages", func() {
				Expect(mgr.IsKnownStream("BASIC_SCALE_UP")).To(BeFalse())

				stream, err := mgr.NewStream("BASIC_SCALE_UP",
					jsm.Subjects("js.in.BASIC_SCALE_UP"),
					jsm.Replicas(1),
					jsm.PlacementCluster("c2"),
					jsm.FileStorage())
				Expect(err).ToNot(HaveOccurred())

				Expect(publishToStream(nc, "js.in.BASIC_SCALE_UP", 1, 100)).To(Succeed())
				Expect(streamMessagesAndSequences(stream, 100)).Should(Succeed())
			})

			It("Should scale up to R3", func() {
				stream, err := mgr.LoadStream("BASIC_SCALE_UP")
				Expect(err).ToNot(HaveOccurred())

				err = stream.UpdateConfiguration(stream.Configuration(), jsm.Replicas(3))
				Expect(err).ToNot(HaveOccurred())

				Eventually(streamLeader(stream), "10s", "1s").Should(BeTrue())
				Eventually(streamClusterReady(stream, 3), "10s", "1s").Should(Succeed())
			})
		})

		Describe("Validate", func() {
			It("Should exist and have messages", func() {
				stream, err := mgr.LoadStream("BASIC_SCALE_UP")
				Expect(err).ToNot(HaveOccurred())

				Eventually(streamClusterReady(stream, 3), "10s", "1s").Should(Succeed())

				nfo, _ := stream.LatestInformation()
				msgs := nfo.State.Msgs
				Expect(msgs).To(BeNumerically(">=", 100))

				Expect(nfo.Config.Replicas).To(Equal(3))
				Expect(nfo.Cluster.Replicas).To(HaveLen(2))

				Expect(publishToStream(nc, "js.in.BASIC_SCALE_UP", int(msgs+1), 100)).To(Succeed())
				Expect(streamMessagesAndSequences(stream, int(msgs+100))).Should(Succeed())
			})
		})
	})
})
