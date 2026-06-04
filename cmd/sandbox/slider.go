package sandbox

import (
	"fmt"
	"strings"

	"atomicgo.dev/keyboard"
	"atomicgo.dev/keyboard/keys"
	"github.com/pterm/pterm"
)

// pickRechargeAmountGB renders an in-terminal slider keyed by ← / →
// (1 GB steps) and ⇧← / ⇧→ (10 GB steps). Returns the picked size in
// gigabytes, or 0 on cancel (Esc / Ctrl+C / q).
//
// The slider is bounded — min 1 GB, max 100 GB — to keep accidental
// hold-down-arrow from blowing through user wallets. Operators who
// want bigger top-ups can still pass the amount as a positional
// (e.g. `bandwidth-recharge my-box 500GB`).
func pickRechargeAmountGB(initialGB int) (int, error) {
	const (
		minGB = 1
		maxGB = 100
	)
	value := initialGB
	if value < minGB {
		value = minGB
	}
	if value > maxGB {
		value = maxGB
	}

	pterm.Println()
	pterm.Println(pterm.Gray("  ← / →  1 GB    ↑ / ↓  10 GB    enter to confirm    esc to cancel"))

	// Reserve two lines that we redraw in place. The cursor goes back
	// up two before each redraw so the slider feels alive instead of
	// scrolling the terminal on every keystroke.
	fmt.Println()
	fmt.Println()
	cancelled := false
	committed := false

	render := func() {
		// Move up 2 lines, erase, redraw.
		fmt.Print("\033[2A\033[J")
		bar := renderSliderBar(value, minGB, maxGB, 40)
		pterm.Printfln("  %s  %d GB", bar, value)
		pterm.Println(pterm.Gray("  (use the arrow keys; enter confirms)"))
	}
	render()

	err := keyboard.Listen(func(key keys.Key) (stop bool, err error) {
		switch key.Code {
		case keys.RuneKey:
			switch strings.ToLower(string(key.Runes)) {
			case "q":
				cancelled = true
				return true, nil
			case "+":
				if value < maxGB {
					value++
				}
			case "-":
				if value > minGB {
					value--
				}
			default:
				return false, nil
			}
		case keys.Right:
			step := 1
			if key.AltPressed {
				step = 10
			}
			value += step
			if value > maxGB {
				value = maxGB
			}
		case keys.Left:
			step := 1
			if key.AltPressed {
				step = 10
			}
			value -= step
			if value < minGB {
				value = minGB
			}
		case keys.Up:
			value += 10
			if value > maxGB {
				value = maxGB
			}
		case keys.Down:
			value -= 10
			if value < minGB {
				value = minGB
			}
		case keys.Enter:
			committed = true
			return true, nil
		case keys.Esc, keys.CtrlC:
			cancelled = true
			return true, nil
		default:
			return false, nil
		}
		render()
		return false, nil
	})
	if err != nil {
		return 0, fmt.Errorf("could not read your input: %w", err)
	}
	if cancelled || !committed {
		return 0, nil
	}
	return value, nil
}

// renderSliderBar draws a width-character bar with a marker at the
// current value's position.
func renderSliderBar(value, lo, hi, width int) string {
	if width < 3 {
		width = 3
	}
	if value < lo {
		value = lo
	}
	if value > hi {
		value = hi
	}
	pos := 0
	if hi > lo {
		pos = ((value - lo) * (width - 1)) / (hi - lo)
	}
	var b strings.Builder
	b.WriteString("[")
	for i := 0; i < width; i++ {
		switch {
		case i == pos:
			b.WriteString("●")
		case i < pos:
			b.WriteString("─")
		default:
			b.WriteString("·")
		}
	}
	b.WriteString("]")
	return b.String()
}
