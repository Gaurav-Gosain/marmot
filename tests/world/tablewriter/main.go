package main

import (
	"os"

	"github.com/olekukonko/tablewriter"
)

func main() {
	t := tablewriter.NewWriter(os.Stdout)
	t.SetHeader([]string{"Name", "Qty", "Price", "Notes"})
	t.SetFooter([]string{"", "", "Total", "$34.20"})
	t.SetBorder(true)
	t.Append([]string{"apple", "3", "$1.50", "red, crisp"})
	t.Append([]string{"watermelon", "1", "$12.00", "a very long note that will be wrapped around somewhere"})
	t.Append([]string{"日本語", "12", "$0.20", "é"})
	t.Render()
	t2 := tablewriter.NewWriter(os.Stdout)
	t2.SetAutoWrapText(false)
	t2.SetAlignment(tablewriter.ALIGN_RIGHT)
	t2.SetColumnSeparator("|")
	t2.SetRowSeparator("-")
	t2.SetCenterSeparator("+")
	t2.SetHeader([]string{"a", "b"})
	t2.AppendBulk([][]string{{"1", "22"}, {"333", "4"}})
	t2.Render()
	t3 := tablewriter.NewWriter(os.Stdout)
	t3.SetBorder(false)
	t3.SetCenterSeparator("")
	t3.SetColumnSeparator("")
	t3.SetRowSeparator("")
	t3.SetHeaderLine(false)
	t3.SetTablePadding("  ")
	t3.SetNoWhiteSpace(true)
	t3.Append([]string{"x", "y"})
	t3.Render()
	t4 := tablewriter.NewWriter(os.Stdout)
	t4.SetAutoMergeCells(true)
	t4.SetRowLine(true)
	t4.Append([]string{"g", "1"})
	t4.Append([]string{"g", "2"})
	t4.Append([]string{"h", "2"})
	t4.Render()
}
