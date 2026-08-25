package output

import (
	"testing"

	"github.com/pterm/pterm"
)

func TestFitTableTruncatesToWidth(t *testing.T) {
	data := pterm.TableData{
		{"ID", "Name", "Status"},
		{"sb-01m0vqbdbmzctm5bw7ms2b44s9h", "jovial-northcutt-xm9z", "running"},
	}

	fitted := FitTable(data, 40)
	widths := columnWidths(fitted, maxColumns(fitted))
	if got := tableWidth(widths); got > 40 {
		t.Fatalf("table width = %d, want <= 40; data = %#v", got, fitted)
	}

	if fitted[1][0] == data[1][0] {
		t.Fatalf("expected long ID to be truncated")
	}
}

func TestFitTableLeavesSmallTableAlone(t *testing.T) {
	data := pterm.TableData{
		{"ID", "Name"},
		{"sb-1", "demo"},
	}

	fitted := FitTable(data, 80)
	if fitted[1][0] != "sb-1" || fitted[1][1] != "demo" {
		t.Fatalf("unexpected fitted table: %#v", fitted)
	}
}

func TestUsableTableWidthLeavesPadding(t *testing.T) {
	if got := usableTableWidth(100); got != 98 {
		t.Fatalf("usableTableWidth(100) = %d, want 98", got)
	}
}
