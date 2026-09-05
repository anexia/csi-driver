package server

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Run", func() {
	It("stops serving and returns nil when the context is cancelled", func() {
		s, err := New(Options{Endpoint: "tcp://127.0.0.1:0"})
		Expect(err).NotTo(HaveOccurred())

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- s.Run(ctx) }()

		cancel()

		Eventually(done).WithTimeout(5 * time.Second).Should(Receive(BeNil()))
	})

	It("returns the serve error when the listener breaks", func() {
		s, err := New(Options{Endpoint: "tcp://127.0.0.1:0"})
		Expect(err).NotTo(HaveOccurred())

		done := make(chan error, 1)
		go func() { done <- s.Run(context.Background()) }()

		Expect(s.(*server).listener.Close()).To(Succeed())

		Eventually(done).WithTimeout(5 * time.Second).Should(Receive(HaveOccurred()))
	})
})
