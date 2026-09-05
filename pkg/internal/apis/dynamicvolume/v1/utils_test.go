package v1

import (
	"strconv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("utils", func() {
	Context("mapPointerSlice", func() {
		It("returns nil if input slice is nil", func() {
			res := mapPointerSlice(func(v int) int { return v * 2 }, nil)
			Expect(res).To(BeNil())
		})

		It("returns slices mapped by map function", func() {
			res := mapPointerSlice(strconv.Itoa, &[]int{0, 1, 2})
			Expect(res).To(Equal(&[]string{"0", "1", "2"}))
		})
	})

	Context("joinPointerString", func() {
		It("returns nil if the input slice is nil", func() {
			res := joinPointerString(nil, ",")
			Expect(res).To(BeNil())
		})

		It("returns a pointer to a joined string of the input slice", func() {
			res := joinPointerString(&[]string{"a", "b", "c"}, " + ")
			expectedString := "a + b + c"
			Expect(res).To(Equal(&expectedString))
		})
	})
})
