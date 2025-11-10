package layout

import (
	"strconv"
	"strings"
)

// This file defines unit-safe types and helpers for length and line-height.

// Unit represents the original unit of a length value as specified in DSL.
type Unit int

const (
	UnitNone Unit = iota // unit-less numbers like factors
	UnitMM               // millimeters
	UnitCM               // centimeters
	UnitIN               // inches
	UnitPT               // points
)

// Conversion constants between pt and mm.
const (
	PtToMm = 0.352777
	MmToPt = 1.0 / PtToMm
)

// UnitToString returns a short string for a Unit value.
func UnitToString(u Unit) string {
	switch u {
	case UnitMM:
		return "mm"
	case UnitCM:
		return "cm"
	case UnitIN:
		return "in"
	case UnitPT:
		return "pt"
	case UnitNone:
		return ""
	default:
		return ""
	}
}

// Length preserves a numeric value with its unit.
type Length struct {
	Value float64 `json:"value"`
	Unit  Unit    `json:"unit"`
}

func (l Length) IsZero() bool { return l.Value == 0 }

// To converts this length to target unit. Supported targets: UnitMM, UnitPT.
func (l Length) To(target Unit) float64 {
	switch l.Unit {
	case UnitMM:
		if target == UnitMM || target == UnitNone {
			return l.Value
		}
		if target == UnitPT {
			return l.Value * MmToPt
		}
	case UnitCM:
		mm := l.Value * 10
		if target == UnitMM || target == UnitNone {
			return mm
		}
		if target == UnitPT {
			return mm * MmToPt
		}
	case UnitIN:
		mm := l.Value * 25.4
		if target == UnitMM || target == UnitNone {
			return mm
		}
		if target == UnitPT {
			return mm * MmToPt
		}
	case UnitPT:
		if target == UnitPT {
			return l.Value
		}
		if target == UnitMM || target == UnitNone {
			return l.Value * PtToMm
		}
	case UnitNone:
		// Treat as same numeric in target if needed by caller; usually not used for absolute lengths.
		return l.Value
	}
	// Default fall back to numeric value as-is
	return l.Value
}

func (l Length) ToMM() float64 { return l.To(UnitMM) }
func (l Length) ToPT() float64 { return l.To(UnitPT) }

// ParseRawLengthStr parses a DSL length string preserving its unit.
func ParseRawLengthStr(value string) Length {
	v := strings.TrimSpace(value)
	if v == "" {
		return Length{Value: 0, Unit: UnitNone}
	}
	lower := strings.ToLower(v)
	unit := UnitNone
	num := lower
	for _, suf := range []struct{
		s string
		u Unit
	}{{"mm", UnitMM}, {"cm", UnitCM}, {"in", UnitIN}, {"pt", UnitPT}} {
		if strings.HasSuffix(lower, suf.s) {
			unit = suf.u
			num = strings.TrimSpace(strings.TrimSuffix(lower, suf.s))
			break
		}
	}
	if unit == UnitNone {
		// default to mm if no unit specified for absolute lengths (rare in DSL); keep NONE to signal unknown.
	}
	f, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return Length{Value: 0, Unit: UnitNone}
	}
	return Length{Value: f, Unit: unit}
}

// LineHeightKind distinguishes factor-based vs absolute line-height specification.
type LineHeightKind int

const (
	LineHeightFactor LineHeightKind = iota
	LineHeightAbsolute
)

// LineHeightSpec preserves original author intent: either a factor (e.g., 1.2x) or an absolute length (e.g., 18pt).
type LineHeightSpec struct {
	Kind   LineHeightKind `json:"kind"`
	Factor float64        `json:"factor,omitempty"`
	Len    Length         `json:"len,omitempty"`
}

// Resolve computes the absolute line height in target unit using the given fontSize (which carries its unit).
func (s LineHeightSpec) Resolve(fontSize Length, target Unit) float64 {
	switch s.Kind {
	case LineHeightFactor:
		// lineHeight = fontSize * factor
		return fontSize.To(target) * s.Factor
	case LineHeightAbsolute:
		return s.Len.To(target)
	default:
		// fallback to 1.4x if unspecified
		return fontSize.To(target) * 1.4
	}
}
