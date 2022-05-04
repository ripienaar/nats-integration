package streams

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/nats-io/jsm.go"
	"github.com/nats-io/nats.go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Tests a stream and it's mirror:
//
// Create:
// - Sets up BASIC stream in c2 cluster
// - Put 100 messages into it
// - Adds a durable but dont use it

// Verify:
// - Checks it's in the right cluster
// - Checks 100 messages
// - Checks the durable exist with the right pending etc
// - Create a nats.go ephemeral consumer and reads the messages
var _ = Describe("Basic Push Consumer", Ordered, func() {
	var (
		nc  *nats.Conn
		mgr *jsm.Manager
		err error
	)

	BeforeEach(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()

		nc, mgr, err = connectUser(ctx)
		Expect(err).ToNot(HaveOccurred())

		// wait for meta cluster
		sysnc, err := connectSystem(ctx)
		Expect(err).ToNot(HaveOccurred())
		Eventually(metaClusterReady(sysnc), "20s", "1s").Should(BeTrue())
	})

	Describe("Basic R1 Stream", Ordered, func() {
		Describe("Create", func() {
			It("Should create and publish message into c2", func() {
				if os.Getenv("VALIDATE_ONLY") != "" {
					Skip("Validating only")
				}

				Expect(mgr.IsKnownStream("BASIC")).To(BeFalse())

				stream, err := mgr.NewStream("BASIC",
					jsm.Subjects("js.in.BASIC"),
					jsm.Replicas(1),
					jsm.MaxBytes(1000000000),
					jsm.PlacementCluster("c2"),
					jsm.FileStorage())
				Expect(err).ToNot(HaveOccurred())

				Expect(publishToStream(nc, "js.in.BASIC", 1, 100)).To(Succeed())
				Expect(streamMessagesAndSequences(stream, 100)).Should(Succeed())

				_, err = stream.NewConsumer(jsm.DurableName("DURABLE"))
				Expect(err).ToNot(HaveOccurred())
			})
		})

		Describe("Validate", func() {
			It("Should exist and have messages", func() {
				stream, err := mgr.LoadStream("BASIC")
				Expect(err).ToNot(HaveOccurred())

				nfo, _ := stream.LatestInformation()
				msgs := int(nfo.State.Msgs)
				Expect(msgs).To(BeNumerically(">=", 100))
				Expect(nfo.Cluster.Name).To(Equal("c2"))

				consumer, err := stream.LoadConsumer("DURABLE")
				Expect(err).ToNot(HaveOccurred())
				cnfo, _ := consumer.State()
				Expect(cnfo.NumPending).To(Equal(uint64(100)))
				Expect(cnfo.Delivered.Consumer).To(Equal(uint64(0)))

				js, err := nc.JetStream()
				Expect(err).ToNot(HaveOccurred())

				sub, err := js.SubscribeSync("js.in.BASIC")
				Expect(err).ToNot(HaveOccurred())

				for i := 1; i < msgs+1; i++ {
					msg, err := sub.NextMsg(time.Second)
					Expect(err).ToNot(HaveOccurred())
					Expect(msg.Data).To(Equal([]byte(fmt.Sprintf("%d", i))))
				}
			})
		})
	})
})
