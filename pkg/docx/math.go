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
	"fmt"
	"strings"
	"unicode"
)

// LaTeX maths as Word equations (Office Math, OMML), so a document's
// formulas stay formulas Word can edit: fractions, roots, sub- and
// superscripts, sums and integrals with their limits, accents, fonts
// (\mathbf, \mathcal, \mathbb, \text), \left…\right, matrices, cases and
// aligned equations, Greek and the usual symbols. A command it doesn't
// know is kept as written, never dropped.

// maxMathDepth bounds nesting, so a hostile formula can't exhaust the stack.
const maxMathDepth = 64

// omml is a formula's equation, without the m:oMath around it.
func omml(latex string) string {
	p := &mathParser{toks: tokenize(latex)}
	return p.expr(nil, mathStyle{})
}

type tokKind int

const (
	tChar  tokKind = iota // a letter, a symbol
	tNum                  // digits and a decimal point
	tCmd                  // \name, or \ and one other character
	tOpen                 // {
	tClose                // }
	tSup                  // ^
	tSub                  // _
	tAmp                  // &
	tRow                  // \\
	tSpace                // white space (kept only in \text)
)

type token struct {
	kind tokKind
	s    string
}

func tokenize(s string) []token {
	var out []token
	r := []rune(s)
	for i := 0; i < len(r); i++ {
		c := r[i]
		switch {
		case c == '\\':
			if i+1 >= len(r) {
				out = append(out, token{tChar, "\\"})
				continue
			}
			if r[i+1] == '\\' {
				out = append(out, token{tRow, ""})
				i++
				continue
			}
			j := i + 1
			if unicode.IsLetter(r[j]) {
				for j < len(r) && unicode.IsLetter(r[j]) {
					j++
				}
			} else {
				j++
			}
			out = append(out, token{tCmd, string(r[i+1 : j])})
			i = j - 1
		case c == '{':
			out = append(out, token{tOpen, "{"})
		case c == '}':
			out = append(out, token{tClose, "}"})
		case c == '^':
			out = append(out, token{tSup, "^"})
		case c == '_':
			out = append(out, token{tSub, "_"})
		case c == '&':
			out = append(out, token{tAmp, "&"})
		case unicode.IsSpace(c):
			out = append(out, token{tSpace, " "})
		case unicode.IsDigit(c):
			j := i
			for j < len(r) && (unicode.IsDigit(r[j]) || r[j] == '.' && j+1 < len(r) && unicode.IsDigit(r[j+1])) {
				j++
			}
			out = append(out, token{tNum, string(r[i:j])})
			i = j - 1
		default:
			out = append(out, token{tChar, string(c)})
		}
	}
	return out
}

// mathStyle is how a run is set: plain (upright), bold, a script.
type mathStyle struct {
	sty  string // "p", "b", "i", "bi" or "" (Word's default: italic letters)
	scr  string // "script", "double-struck", "fraktur", "sans-serif", "monospace"
	text bool   // \text: words, spaces kept
}

type mathParser struct {
	toks  []token
	i     int
	depth int
}

func (p *mathParser) peek() (token, bool) {
	if p.i < len(p.toks) {
		return p.toks[p.i], true
	}
	return token{}, false
}

func (p *mathParser) next() (token, bool) {
	t, ok := p.peek()
	if ok {
		p.i++
	}
	return t, ok
}

func (p *mathParser) skipSpace() {
	for t, ok := p.peek(); ok && t.kind == tSpace; t, ok = p.peek() {
		p.i++
	}
}

// expr reads atoms until one of stop (a closing brace, &, \\, \right,
// \end) or the end, without consuming it.
func (p *mathParser) expr(stop func(token) bool, st mathStyle) string {
	var b strings.Builder
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > maxMathDepth {
		p.i = len(p.toks)
		return ""
	}
	for {
		p.skipSpace()
		t, ok := p.peek()
		if !ok || (stop != nil && stop(t)) {
			return b.String()
		}
		if t.kind == tClose && stop == nil {
			p.i++ // a stray }
			continue
		}
		b.WriteString(p.scripted(st))
	}
}

// scripted is an atom with its sub- and superscripts, and primes.
func (p *mathParser) scripted(st mathStyle) string {
	t, _ := p.peek()
	if t.kind == tCmd && naryOps[t.s] != "" {
		return p.nary(st)
	}
	if t.kind == tCmd && limitFuncs[t.s] {
		p.i++
		name := run(t.s, mathStyle{sty: "p"})
		if sub, _, ok := p.scripts(st); ok && sub != "" {
			return "<m:limLow><m:e>" + name + "</m:e><m:lim>" + sub + "</m:lim></m:limLow>"
		}
		return name
	}
	base := p.atom(st)
	sub, sup, ok := p.scripts(st)
	switch {
	case !ok:
		return base
	case sub != "" && sup != "":
		return "<m:sSubSup><m:e>" + base + "</m:e><m:sub>" + sub + "</m:sub><m:sup>" + sup + "</m:sup></m:sSubSup>"
	case sub != "":
		return "<m:sSub><m:e>" + base + "</m:e><m:sub>" + sub + "</m:sub></m:sSub>"
	case sup != "":
		return "<m:sSup><m:e>" + base + "</m:e><m:sup>" + sup + "</m:sup></m:sSup>"
	}
	return base
}

// scripts reads _x, ^y (either order) and primes after an atom.
func (p *mathParser) scripts(st mathStyle) (sub, sup string, ok bool) {
	for {
		p.skipSpace()
		t, more := p.peek()
		if !more {
			return
		}
		switch {
		case t.kind == tSub:
			p.i++
			sub, ok = p.arg(st), true
		case t.kind == tSup:
			p.i++
			sup, ok = sup+p.arg(st), true
		case t.kind == tChar && t.s == "'":
			p.i++
			sup, ok = sup+run("′", mathStyle{sty: "p"}), true
		case t.kind == tCmd && (t.s == "limits" || t.s == "nolimits"):
			p.i++
		default:
			return
		}
	}
}

// arg is a command's argument: a {group}, or one atom.
func (p *mathParser) arg(st mathStyle) string {
	p.skipSpace()
	t, ok := p.peek()
	if !ok {
		return ""
	}
	if t.kind == tOpen {
		p.i++
		s := p.expr(func(t token) bool { return t.kind == tClose }, st)
		p.i++ // }
		return s
	}
	return p.atom(st)
}

// rawArg is a {group}'s text as written (an environment's name, \text's words).
func (p *mathParser) rawArg() string {
	p.skipSpace()
	t, ok := p.peek()
	if !ok || t.kind != tOpen {
		if ok {
			p.i++
			return t.s
		}
		return ""
	}
	p.i++
	var b strings.Builder
	depth := 1
	for t, ok := p.next(); ok; t, ok = p.next() {
		switch t.kind {
		case tOpen:
			depth++
			continue
		case tClose:
			if depth--; depth == 0 {
				return b.String()
			}
			continue
		case tCmd:
			if sym, ok := symbols[t.s]; ok {
				b.WriteString(sym)
				continue
			}
			b.WriteString(t.s)
			continue
		case tRow:
			b.WriteString(" ")
			continue
		}
		b.WriteString(t.s)
	}
	return b.String()
}

func (p *mathParser) atom(st mathStyle) string {
	t, ok := p.next()
	if !ok {
		return ""
	}
	switch t.kind {
	case tOpen:
		s := p.expr(func(t token) bool { return t.kind == tClose }, st)
		p.i++
		return s
	case tNum:
		return run(t.s, mathStyle{sty: "p", scr: st.scr})
	case tChar:
		if t.s == "-" {
			return run("−", mathStyle{sty: "p"})
		}
		if unicode.IsLetter([]rune(t.s)[0]) {
			return run(t.s, st)
		}
		return run(t.s, mathStyle{sty: "p"})
	case tAmp, tRow, tClose:
		return ""
	case tSup, tSub:
		return p.arg(st) // a script with nothing before it
	}
	return p.command(t.s, st)
}

func (p *mathParser) command(name string, st mathStyle) string {
	switch name {
	case "frac", "dfrac", "tfrac", "cfrac":
		num, den := p.arg(st), p.arg(st)
		return "<m:f><m:num>" + num + "</m:num><m:den>" + den + "</m:den></m:f>"
	case "binom", "dbinom", "tbinom":
		num, den := p.arg(st), p.arg(st)
		return delimited("(", ")", `<m:f><m:fPr><m:type m:val="noBar"/></m:fPr><m:num>`+num+"</m:num><m:den>"+den+"</m:den></m:f>")
	case "sqrt":
		deg := ""
		p.skipSpace()
		if t, ok := p.peek(); ok && t.kind == tChar && t.s == "[" {
			p.i++
			deg = p.expr(func(t token) bool { return t.kind == tChar && t.s == "]" }, st)
			p.i++
		}
		e := p.arg(st)
		if deg == "" {
			return `<m:rad><m:radPr><m:degHide m:val="1"/></m:radPr><m:deg/><m:e>` + e + "</m:e></m:rad>"
		}
		return "<m:rad><m:deg>" + deg + "</m:deg><m:e>" + e + "</m:e></m:rad>"
	case "left":
		open := p.delim()
		inner := p.expr(func(t token) bool { return t.kind == tCmd && t.s == "right" }, st)
		p.i++ // \right
		return delimited(open, p.delim(), inner)
	case "right":
		p.delim()
		return ""
	case "begin":
		return p.environment(p.rawArg(), st)
	case "end":
		p.rawArg()
		return ""
	case "text", "textrm", "textnormal", "mbox", "textup":
		return run(p.rawArg(), mathStyle{sty: "p", text: true})
	case "textbf":
		return run(p.rawArg(), mathStyle{sty: "b", text: true})
	case "textit", "emph":
		return run(p.rawArg(), mathStyle{sty: "i", text: true})
	case "operatorname":
		return run(p.rawArg(), mathStyle{sty: "p"})
	case "overline":
		return `<m:bar><m:barPr><m:pos m:val="top"/></m:barPr><m:e>` + p.arg(st) + "</m:e></m:bar>"
	case "underline":
		return "<m:bar><m:e>" + p.arg(st) + "</m:e></m:bar>"
	case "displaystyle", "textstyle", "scriptstyle", "limits", "nolimits", "nonumber", "notag", "label":
		if name == "label" {
			p.rawArg()
		}
		return ""
	case "tag":
		return run("("+p.rawArg()+")", mathStyle{sty: "p"})
	}
	if s, ok := fonts[name]; ok {
		return p.arg(s)
	}
	if acc, ok := accents[name]; ok {
		return `<m:acc><m:accPr><m:chr m:val="` + acc + `"/></m:accPr><m:e>` + p.arg(st) + "</m:e></m:acc>"
	}
	if sp, ok := spaces[name]; ok {
		return run(sp, mathStyle{sty: "p", text: true})
	}
	if funcs[name] {
		return run(name, mathStyle{sty: "p"})
	}
	if sym, ok := symbols[name]; ok {
		s := mathStyle{sty: "p"}
		if unicode.IsLetter([]rune(sym)[0]) && unicode.IsLower([]rune(sym)[0]) {
			s = st // lower-case Greek is italic, as letters are
		}
		return run(sym, s)
	}
	return run(`\`+name, mathStyle{sty: "p"}) // kept as written
}

// delim is the delimiter after \left or \right ("" for ".").
func (p *mathParser) delim() string {
	p.skipSpace()
	t, ok := p.next()
	if !ok {
		return ""
	}
	switch {
	case t.kind == tChar && t.s == ".":
		return ""
	case t.kind == tCmd:
		if d, ok := delimiters[t.s]; ok {
			return d
		}
		return t.s
	}
	return t.s
}

// nary is a sum, product or integral with its limits and what it applies to.
func (p *mathParser) nary(st mathStyle) string {
	t, _ := p.next()
	chr := naryOps[t.s]
	sub, sup, _ := p.scripts(st)
	loc := "undOvr"
	if strings.Contains("∫∬∭∮", chr) {
		loc = "subSup"
	}
	var pr strings.Builder
	fmt.Fprintf(&pr, `<m:naryPr><m:chr m:val="%s"/><m:limLoc m:val="%s"/>`, chr, loc)
	if sub == "" {
		pr.WriteString(`<m:subHide m:val="1"/>`)
	}
	if sup == "" {
		pr.WriteString(`<m:supHide m:val="1"/>`)
	}
	pr.WriteString("</m:naryPr>")
	p.skipSpace()
	e := ""
	if t, ok := p.peek(); ok && t.kind != tClose && t.kind != tAmp && t.kind != tRow && !(t.kind == tCmd && (t.s == "right" || t.s == "end")) {
		e = p.scripted(st)
	}
	return "<m:nary>" + pr.String() + "<m:sub>" + sub + "</m:sub><m:sup>" + sup + "</m:sup><m:e>" + e + "</m:e></m:nary>"
}

// environment is a matrix, cases or aligned equations, to its \end.
func (p *mathParser) environment(name string, st mathStyle) string {
	stop := func(t token) bool {
		return t.kind == tAmp || t.kind == tRow || (t.kind == tCmd && t.s == "end")
	}
	if name == "array" {
		p.rawArg() // the column spec
	}
	var rows [][]string
	row := []string{}
	for {
		cell := p.expr(stop, st)
		row = append(row, cell)
		t, ok := p.next()
		if !ok {
			break
		}
		if t.kind == tAmp {
			continue
		}
		rows = append(rows, row)
		row = []string{}
		if t.kind == tCmd { // \end
			p.rawArg()
			break
		}
	}
	if len(row) > 1 || (len(row) == 1 && row[0] != "") {
		rows = append(rows, row)
	}
	name = strings.TrimSuffix(name, "*")
	switch name {
	case "aligned", "align", "gathered", "gather", "split", "alignat", "eqnarray", "multline":
		var b strings.Builder
		b.WriteString("<m:eqArr>")
		for _, r := range rows {
			b.WriteString("<m:e>" + strings.Join(r, "") + "</m:e>")
		}
		b.WriteString("</m:eqArr>")
		return b.String()
	}
	cols := 0
	for _, r := range rows {
		cols = max(cols, len(r))
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<m:m><m:mPr><m:mcs><m:mc><m:mcPr><m:count m:val="%d"/><m:mcJc m:val="%s"/></m:mcPr></m:mc></m:mcs></m:mPr>`, max(cols, 1), map[bool]string{true: "left", false: "center"}[name == "cases"])
	for _, r := range rows {
		b.WriteString("<m:mr>")
		for c := range cols {
			cell := ""
			if c < len(r) {
				cell = r[c]
			}
			b.WriteString("<m:e>" + cell + "</m:e>")
		}
		b.WriteString("</m:mr>")
	}
	b.WriteString("</m:m>")
	pair := map[string][2]string{
		"pmatrix": {"(", ")"}, "bmatrix": {"[", "]"}, "Bmatrix": {"{", "}"}, "vmatrix": {"|", "|"}, "Vmatrix": {"‖", "‖"}, "cases": {"{", ""},
	}[name]
	open, closing := pair[0], pair[1]
	if open == "" && closing == "" {
		return b.String()
	}
	return delimited(open, closing, b.String())
}

func delimited(open, closing, inner string) string {
	return `<m:d><m:dPr><m:begChr m:val="` + escape(open) + `"/><m:endChr m:val="` + escape(closing) + `"/></m:dPr><m:e>` + inner + "</m:e></m:d>"
}

// run is a maths run of text in a style. Text (\text) is "normal text",
// which Office Math sets without its maths styles, so its bold or italic
// is the run's own formatting.
func run(s string, st mathStyle) string {
	if s == "" {
		return ""
	}
	out := "<m:r>"
	switch {
	case st.text:
		out += "<m:rPr><m:nor/></m:rPr>"
		switch st.sty {
		case "b":
			out += "<w:rPr><w:b/></w:rPr>"
		case "i":
			out += "<w:rPr><w:i/></w:rPr>"
		}
	case st.scr != "" || st.sty != "":
		out += "<m:rPr>"
		if st.scr != "" {
			out += `<m:scr m:val="` + st.scr + `"/>`
		}
		if st.sty != "" {
			out += `<m:sty m:val="` + st.sty + `"/>`
		}
		out += "</m:rPr>"
	}
	return out + `<m:t xml:space="preserve">` + escape(s) + "</m:t></m:r>"
}

var fonts = map[string]mathStyle{
	"mathbf": {sty: "b"}, "boldsymbol": {sty: "bi"}, "bm": {sty: "bi"}, "mathrm": {sty: "p"}, "mathit": {sty: "i"},
	"mathcal": {scr: "script", sty: "p"}, "mathscr": {scr: "script", sty: "p"}, "mathbb": {scr: "double-struck", sty: "p"},
	"mathfrak": {scr: "fraktur", sty: "p"}, "mathsf": {scr: "sans-serif", sty: "p"}, "mathtt": {scr: "monospace", sty: "p"},
}

var accents = map[string]string{
	"hat": "̂", "widehat": "̂", "bar": "̅", "tilde": "̃", "widetilde": "̃", "vec": "⃗",
	"dot": "̇", "ddot": "̈", "check": "̌", "breve": "̆", "acute": "́", "grave": "̀",
}

var spaces = map[string]string{
	",": " ", ":": " ", ";": " ", "!": "", " ": " ", "quad": " ", "qquad": "  ", "enspace": " ",
}

var naryOps = map[string]string{
	"sum": "∑", "prod": "∏", "coprod": "∐", "int": "∫", "iint": "∬", "iiint": "∭", "oint": "∮",
	"bigcup": "⋃", "bigcap": "⋂", "bigoplus": "⨁", "bigotimes": "⨂", "bigvee": "⋁", "bigwedge": "⋀",
}

var limitFuncs = map[string]bool{"lim": true, "max": true, "min": true, "sup": true, "inf": true, "argmax": true, "argmin": true, "limsup": true, "liminf": true}

var funcs = map[string]bool{
	"sin": true, "cos": true, "tan": true, "cot": true, "sec": true, "csc": true, "arcsin": true, "arccos": true, "arctan": true,
	"sinh": true, "cosh": true, "tanh": true, "log": true, "ln": true, "lg": true, "exp": true, "det": true, "dim": true,
	"ker": true, "deg": true, "gcd": true, "hom": true, "arg": true, "Pr": true, "tr": true, "rank": true, "diag": true,
}

var delimiters = map[string]string{
	"{": "{", "}": "}", "langle": "⟨", "rangle": "⟩", "lvert": "|", "rvert": "|", "lVert": "‖", "rVert": "‖", "|": "‖",
	"lfloor": "⌊", "rfloor": "⌋", "lceil": "⌈", "rceil": "⌉", "vert": "|", "Vert": "‖",
}

var symbols = map[string]string{
	// Greek
	"alpha": "α", "beta": "β", "gamma": "γ", "delta": "δ", "epsilon": "ϵ", "varepsilon": "ε", "zeta": "ζ", "eta": "η",
	"theta": "θ", "vartheta": "ϑ", "iota": "ι", "kappa": "κ", "lambda": "λ", "mu": "μ", "nu": "ν", "xi": "ξ", "pi": "π",
	"varpi": "ϖ", "rho": "ρ", "varrho": "ϱ", "sigma": "σ", "varsigma": "ς", "tau": "τ", "upsilon": "υ", "phi": "ϕ",
	"varphi": "φ", "chi": "χ", "psi": "ψ", "omega": "ω", "Gamma": "Γ", "Delta": "Δ", "Theta": "Θ", "Lambda": "Λ",
	"Xi": "Ξ", "Pi": "Π", "Sigma": "Σ", "Upsilon": "Υ", "Phi": "Φ", "Psi": "Ψ", "Omega": "Ω",
	// operators and relations
	"pm": "±", "mp": "∓", "times": "×", "div": "÷", "cdot": "⋅", "ast": "∗", "star": "⋆", "circ": "∘", "bullet": "∙",
	"oplus": "⊕", "otimes": "⊗", "le": "≤", "leq": "≤", "ge": "≥", "geq": "≥", "neq": "≠", "ne": "≠", "approx": "≈",
	"equiv": "≡", "sim": "∼", "simeq": "≃", "cong": "≅", "propto": "∝", "ll": "≪", "gg": "≫", "in": "∈", "notin": "∉",
	"ni": "∋", "subset": "⊂", "supset": "⊃", "subseteq": "⊆", "supseteq": "⊇", "cup": "∪", "cap": "∩", "setminus": "∖",
	"wedge": "∧", "land": "∧", "vee": "∨", "lor": "∨", "neg": "¬", "lnot": "¬", "forall": "∀", "exists": "∃",
	"mid": "∣", "parallel": "∥", "perp": "⊥", "models": "⊨", "vdash": "⊢",
	// arrows
	"to": "→", "rightarrow": "→", "leftarrow": "←", "gets": "←", "leftrightarrow": "↔", "Rightarrow": "⇒",
	"Leftarrow": "⇐", "Leftrightarrow": "⇔", "implies": "⟹", "iff": "⟺", "mapsto": "↦", "uparrow": "↑",
	"downarrow": "↓", "longrightarrow": "⟶", "longleftarrow": "⟵",
	// misc
	"infty": "∞", "partial": "∂", "nabla": "∇", "emptyset": "∅", "varnothing": "∅", "ell": "ℓ", "hbar": "ℏ",
	"Re": "ℜ", "Im": "ℑ", "aleph": "ℵ", "angle": "∠", "degree": "°", "prime": "′", "dots": "…", "ldots": "…",
	"cdots": "⋯", "vdots": "⋮", "ddots": "⋱", "therefore": "∴", "because": "∵", "top": "⊤", "bot": "⊥",
	"langle": "⟨", "rangle": "⟩", "lfloor": "⌊", "rfloor": "⌋", "lceil": "⌈", "rceil": "⌉",
	"{": "{", "}": "}", "%": "%", "$": "$", "&": "&", "#": "#", "_": "_", "|": "‖",
}
