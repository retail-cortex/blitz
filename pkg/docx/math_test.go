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

package docx

import (
	"encoding/xml"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wellFormedMath checks an equation is well-formed XML, in its namespaces.
func wellFormedMath(t *testing.T, latex, eq string) {
	t.Helper()
	d := xml.NewDecoder(strings.NewReader(`<m:oMath xmlns:m="m" xmlns:w="w">` + eq + `</m:oMath>`))
	for {
		_, err := d.Token()
		if errors.Is(err, io.EOF) {
			return
		}
		require.NoError(t, err, "%s gives %s", latex, eq)
	}
}

func TestOMML(t *testing.T) {
	tests := []struct {
		latex string
		want  []string
	}{
		{`x`, []string{`<m:t xml:space="preserve">x</m:t>`}},
		{`12.5 - x`, []string{`<m:sty m:val="p"/></m:rPr><m:t xml:space="preserve">12.5</m:t>`, `−`}},
		{`\frac{a}{b}`, []string{`<m:f><m:num>`, `<m:den>`}},
		{`\binom{n}{k}`, []string{`<m:type m:val="noBar"/>`, `<m:begChr m:val="("/>`}},
		{`\sqrt{x}`, []string{`<m:degHide m:val="1"/>`}},
		{`\sqrt[3]{x}`, []string{`<m:deg><m:r>`}},
		{`x_i^2`, []string{`<m:sSubSup>`}},
		{`x^{2}`, []string{`<m:sSup>`}},
		{`x_i`, []string{`<m:sSub>`}},
		{`f'`, []string{`<m:sSup>`, `′`}},
		{`\sum_{i=1}^n x_i`, []string{`<m:nary>`, `<m:chr m:val="∑"/>`, `<m:limLoc m:val="undOvr"/>`, `<m:e><m:sSub>`}},
		{`\int_0^1 f`, []string{`<m:chr m:val="∫"/>`, `<m:limLoc m:val="subSup"/>`}},
		{`\prod x`, []string{`<m:subHide m:val="1"/>`, `<m:supHide m:val="1"/>`}},
		{`\lim_{n \to \infty} a_n`, []string{`<m:limLow>`, `→`, `∞`}},
		{`\max x`, []string{`max`}},
		{`\sin x + \log y`, []string{`sin`, `log`}},
		{`\hat{\beta}`, []string{`<m:acc>`, `<m:chr m:val="̂"/>`, `β`}},
		{`\overline{x} \underline{y}`, []string{`<m:pos m:val="top"/>`, `<m:bar><m:e>`}},
		{`\mathbf{X}^T`, []string{`<m:sty m:val="b"/>`}},
		{`\mathcal{L} \mathbb{R}`, []string{`<m:scr m:val="script"/>`, `<m:scr m:val="double-struck"/>`}},
		{`\text{if } x`, []string{`<m:nor/>`, `if `}},
		{`\textbf{b}`, []string{`<m:nor/></m:rPr><w:rPr><w:b/></w:rPr>`}},
		{`\textit{i}`, []string{`<w:rPr><w:i/></w:rPr>`}},
		{`\operatorname{argmax}`, []string{`argmax`}},
		{`\left( \frac{1}{n} \right)`, []string{`<m:d>`, `<m:begChr m:val="("/>`, `<m:endChr m:val=")"/>`}},
		{`\left\langle x \right.`, []string{`<m:begChr m:val="⟨"/>`, `<m:endChr m:val=""/>`}},
		{`\begin{pmatrix} 1 & 2 \\ 3 & 4 \end{pmatrix}`, []string{`<m:m>`, `<m:count m:val="2"/>`, `<m:mr>`, `<m:begChr m:val="("/>`}},
		{`\begin{cases} x & y \\ z \end{cases}`, []string{`<m:begChr m:val="{"/>`, `<m:mcJc m:val="left"/>`}},
		{`\begin{aligned} a &= b \\ c &= d \end{aligned}`, []string{`<m:eqArr>`}},
		{`\begin{array}{cc} a & b \end{array}`, []string{`<m:m>`}},
		{`a \quad b \, c`, []string{" ", " "}},
		{`\alpha \Gamma \le \neq \cdot \{ \}`, []string{`α`, `Γ`, `≤`, `≠`, `⋅`, `{`, `}`}},
		{`\tag{1} \label{eq} \displaystyle x`, []string{`(1)`}},
		{`\unknowncmd x`, []string{`\unknowncmd`}},
		{`a < b & c`, []string{`&lt;`}},
		{`\text x`, []string{`<m:nor/>`, `>x<`}},
		{`\text{a \alpha {b} \\ c}`, []string{`a α b`, ` c<`}},
		{`\mathbf x`, []string{`<m:sty m:val="b"/>`}},
	}
	for _, tt := range tests {
		t.Run(tt.latex, func(t *testing.T) {
			eq := omml(tt.latex)
			wellFormedMath(t, tt.latex, eq)
			for _, w := range tt.want {
				assert.Contains(t, eq, w)
			}
		})
	}
}

// Whatever comes in, the equation is well-formed XML and finishes.
func TestOMMLOddInput(t *testing.T) {
	for _, latex := range []string{
		"", `}`, `{`, `{{{`, `x^`, `_`, `\frac`, `\frac{a}`, `\sqrt[`, `\left(`, `\right)`, `\begin{pmatrix}`, `\end{x}`,
		`\`, `\\`, `&`, `\text{`, `\sum`, `\lim`, `x''`, `\mathbf`, strings.Repeat("{", 200) + "x" + strings.Repeat("}", 200),
		strings.Repeat(`\frac{`, 100), "\x00\x01<>&\"'",
	} {
		wellFormedMath(t, latex, omml(latex))
	}
}

func TestSVGSize(t *testing.T) {
	for _, tt := range []struct {
		svg  string
		w, h int
		ok   bool
	}{
		{`<svg width="200" height="100">`, 200, 100, true},
		{`<svg width="200px" height="100px">`, 200, 100, true},
		{`<?xml version="1.0"?><!-- made by hand --><!DOCTYPE svg><svg viewBox="0 0 320 180">`, 320, 180, true},
		{`<svg width="160" viewBox="0 0 320 180">`, 160, 90, true},
		{`<svg height="90" viewBox="0,0,320,180">`, 160, 90, true},
		{`<svg width="50%">`, 300, 150, true},
		{"\xef\xbb\xbf<svg width=\"10\" height=\"20\">", 10, 20, true},
		{`<html><body><svg width="10" height="10"></svg>`, 0, 0, false},
		{`not a picture`, 0, 0, false},
	} {
		w, h, ok := svgSize([]byte(tt.svg))
		assert.Equal(t, tt.ok, ok, tt.svg)
		assert.Equal(t, [2]int{tt.w, tt.h}, [2]int{w, h}, tt.svg)
	}
}
