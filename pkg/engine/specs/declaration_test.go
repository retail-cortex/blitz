// Copyright 2026 Retail Cortex
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package specs

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The declaration a requirement's evidence sits in, by language (SDD-20).
func TestDeclaration(t *testing.T) {
	goSrc := "package cart\n\n// Pay takes a payment (ORD-01).\n// It fails over 30 s.\nfunc (c *Cart) Pay(ctx context.Context) error {\n\t// ORD-02: queue it.\n\treturn nil\n}\n\ntype Queue struct{}\n"
	for _, tc := range []struct {
		name, path, text string
		line             int
		want             Symbol
		ok               bool
	}{
		{"a comment documents what's below", "cart.go", goSrc, 3, Symbol{Name: "Pay", Path: "cart.go", Line: 5, Column: 16}, true},
		{"inside a body: the one above", "cart.go", goSrc, 6, Symbol{Name: "Pay", Path: "cart.go", Line: 5, Column: 16}, true},
		{"a type", "cart.go", goSrc, 10, Symbol{Name: "Queue", Path: "cart.go", Line: 10, Column: 6}, true},
		{"python", "app/orders.py", "class Orders:\n    # ORD-01\n    async def pay(self):\n        pass\n", 2, Symbol{Name: "pay", Path: "app/orders.py", Line: 3, Column: 15}, true},
		{"typescript arrow", "src/pay.ts", "export const pay = async (id: string): Promise<void> => {\n  // ORD-01\n};\n", 2, Symbol{Name: "pay", Path: "src/pay.ts", Line: 1, Column: 14}, true},
		{"typescript method, not an if", "src/cart.ts", "class Cart {\n  total(): number {\n    if (x) {\n      // ORD-01\n", 4, Symbol{Name: "total", Path: "src/cart.ts", Line: 2, Column: 3}, true},
		{"rust", "src/lib.rs", "pub fn pay() {\n    // ORD-01\n}\n", 2, Symbol{Name: "pay", Path: "src/lib.rs", Line: 1, Column: 8}, true},
		{"a language it can't read", "notes.md", "# ORD-01", 1, Symbol{}, false},
		{"out of range", "cart.go", goSrc, 99, Symbol{}, false},
		{"nothing above", "cart.go", "// ORD-01\nvar x = 1\n", 1, Symbol{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := Declaration(tc.path, tc.text, tc.line)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestUses(t *testing.T) {
	src := "func Pay() {}\nfunc main() {\n\tPay()\n\tPayLater()\n\tc.Pay(x)\n}\n"
	got := Uses("main.go", src, "Pay", 1)
	assert.Equal(t, []Evidence{{Path: "main.go", Line: 3, Kind: "code", Text: "Pay()"}, {Path: "main.go", Line: 5, Kind: "code", Text: "c.Pay(x)"}}, got, "not its declaration, not PayLater")
	assert.Nil(t, Uses("x.go", "nothing", "Pay", 0))
	assert.Equal(t, "test", Uses("main_test.go", "Pay()", "Pay", 0)[0].Kind)
}
