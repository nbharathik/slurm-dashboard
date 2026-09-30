// Package layout decides how the UI adapts to the terminal size: which
// breakpoint applies and how table columns are chosen and sized.
// Every table column has a priority (1 = never dropped) and a minimum
// width; the lowest-priority columns are dropped until the table fits, and
// flexible columns absorb spare width.
package layout
