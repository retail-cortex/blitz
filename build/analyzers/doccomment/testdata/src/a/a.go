// Package a is documented.
package a

// Documented is fine.
func Documented() {}

func Undocumented() {} // want `exported func Undocumented has no doc comment`

func unexported() {}

// T is documented.
type T struct{}

func (T) Method() {} // want `exported method Method has no doc comment`

func (t) method() {}

type t struct{}

func (t) Exported() {} // an exported method of an unexported type: fine

type U int // want `exported type U has no doc comment`

// A group's comment covers its names.
const (
	A = 1
	B = 2
)

var V = 1 // A trailing comment documents it too.

var W = /* want `exported var W has no doc comment` */ 2

//go:noinline
func DirectiveOnly() {} // want `exported func DirectiveOnly has no doc comment`
