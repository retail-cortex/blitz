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

package runtime

import "math"

// A voice's pitch (spec_063_voice VOX-72). Text-to-Speech's Chirp 3: HD
// voices take a speaking rate and no pitch, so the pitch is made here:
// audio read faster than it was made sounds higher (and is shorter by as
// much), slower sounds lower. The speech is asked for that much slower or
// faster (config.VoiceTuning.SpeakingRate), so it comes out at the
// personality's speed, in its pitch.

// resampleTaps is how many of the input's zero crossings each side of a
// sample the filter reads.
const resampleTaps = 12

// pitchShifter reads 16-bit mono PCM at factor times its rate, a piece
// at a time: factor 1.06 is a semitone higher. It filters as it reads (a
// windowed sinc, its band narrowed when reading faster), so nothing
// folds back as hiss.
type pitchShifter struct {
	factor float64
	// in is the input not yet done with, and pos where the next output
	// sample reads in it.
	in  []float64
	pos float64
	// odd is half a sample, when a piece ended mid-sample.
	odd []byte
}

// newPitchShifter is a shifter by factor; nil for 1 (nothing to do) or a
// factor that isn't one.
func newPitchShifter(factor float64) *pitchShifter {
	if math.IsNaN(factor) || factor <= 0 || math.Abs(factor-1) < 1e-3 {
		return nil
	}
	return &pitchShifter{factor: factor}
}

// write is pcm shifted: what can be made of it and what was kept of the
// pieces before; the last of it waits for the next piece, or flush.
func (p *pitchShifter) write(pcm []byte) []byte {
	if len(p.odd) > 0 {
		pcm = append(p.odd, pcm...)
		p.odd = nil
	}
	if len(pcm)%2 == 1 {
		p.odd = []byte{pcm[len(pcm)-1]}
		pcm = pcm[:len(pcm)-1]
	}
	for i := 0; i+1 < len(pcm); i += 2 {
		p.in = append(p.in, float64(int16(uint16(pcm[i])|uint16(pcm[i+1])<<8)))
	}
	return p.read(false)
}

// flush is the rest, the input taken to end in silence.
func (p *pitchShifter) flush() []byte {
	out := p.read(true)
	p.in, p.pos, p.odd = nil, 0, nil
	return out
}

// read makes the output samples whose filter has its input (all of them
// at the end).
func (p *pitchShifter) read(end bool) []byte {
	// Reading faster, the band is narrowed to what the output can hold.
	scale := math.Min(1, 1/p.factor)
	half := float64(resampleTaps) / scale
	limit := float64(len(p.in)) - half - 1
	if end {
		limit = float64(len(p.in)) - 1
	}
	out := make([]byte, 0, int(math.Max(0, limit-p.pos)/p.factor+1)*2)
	for ; p.pos <= limit; p.pos += p.factor {
		lo, hi := int(math.Ceil(p.pos-half)), int(math.Floor(p.pos+half))
		var sum float64
		for n := max(lo, 0); n <= hi && n < len(p.in); n++ {
			sum += p.in[n] * kernel((p.pos-float64(n))*scale, resampleTaps) * scale
		}
		s := int(math.Round(sum))
		s = max(math.MinInt16, min(math.MaxInt16, s))
		out = append(out, byte(s), byte(s>>8))
	}
	// What no later sample reads is let go.
	if drop := int(p.pos-half) - 1; drop > 0 {
		drop = min(drop, len(p.in))
		p.in = append(p.in[:0], p.in[drop:]...)
		p.pos -= float64(drop)
	}
	return out
}

// kernel is a sinc under a Hann window, taps zero crossings wide each
// side.
func kernel(x float64, taps int) float64 {
	if x == 0 {
		return 1
	}
	if math.Abs(x) >= float64(taps) {
		return 0
	}
	px := math.Pi * x
	return math.Sin(px) / px * (0.5 + 0.5*math.Cos(px/float64(taps)))
}
