package main

import (
	"encoding/csv"
	"fmt"
	"log"
	"net/http"
	"os"
	"text/tabwriter"
	"time"

	"github.com/alecthomas/kong"
)

const (
	nyseTradeHaltURL = "https://www.nyse.com/api/trade-halts/current/download"
	bellSound        = "\a"
)

type CLI struct {
	Fetch FetchCmd `cmd:"" help:"Fetch current NYSE trade halts."`
	Watch WatchCmd `cmd:"" help:"Watch for new NYSE trade halts and ding on new halts."`
}

type FetchCmd struct{}

func (f *FetchCmd) Run() error {
	halts, err := fetchTradeHalts()
	if err != nil {
		return fmt.Errorf("failed to fetch trade halts: %w", err)
	}

	displayHaltsTable(halts)
	return nil
}

type WatchCmd struct {
	Interval time.Duration `help:"Polling interval (e.g., 5s, 1m)." default:"5s"`
}

func (w *WatchCmd) Run() error {
	initialHalts, err := fetchTradeHalts()
	if err != nil {
		return fmt.Errorf("failed to fetch initial trade halts: %w", err)
	}

	lastHalts := make(map[string]struct{})
	for _, halt := range initialHalts {
		lastHalts[halt.Symbol] = struct{}{}
	}

	clearScreen()
	displayHaltsTable(initialHalts)

	for range time.Tick(w.Interval) {
		currentHalts, err := fetchTradeHalts()
		if err != nil {
			log.Fatal(err)
		}

		newHalts := false

		for _, halt := range currentHalts {
			if _, found := lastHalts[halt.Symbol]; !found {
				lastHalts[halt.Symbol] = struct{}{}
				newHalts = true
			}
		}

		if newHalts {
			fmt.Print(bellSound)
		}

		clearScreen()
		displayHaltsTable(currentHalts)
		fmt.Printf("\nUpdated @ %s\n", time.Now().Format(time.RFC1123Z))
	}

	return nil
}

func displayHaltsTable(halts []TradeHalt) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SYMBOL\tNAME\tEXCHANGE\tREASON\tHALT DATE\tHALT TIME\tRESUME DATE")
	fmt.Fprintln(w, "------\t----\t--------\t------\t---------\t---------\t-----------")

	for _, halt := range halts {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			halt.Symbol, halt.Name, halt.Exchange, halt.Reason,
			halt.HaltDate, halt.HaltTime, halt.ResumeDate)
	}
	w.Flush()
}

func clearScreen() {
	fmt.Print("\033[2J\033[H")
}

type TradeHalt struct {
	HaltDate   string
	HaltTime   string
	Symbol     string
	Name       string
	Exchange   string
	Reason     string
	ResumeDate string
	NYSETime   string
}

func fetchTradeHalts() ([]TradeHalt, error) {
	resp, err := http.Get(nyseTradeHaltURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch trade halts: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bad status code: %d", resp.StatusCode)
	}

	reader := csv.NewReader(resp.Body)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to read csv: %w", err)
	}

	if len(records) < 2 {
		return []TradeHalt{}, nil
	}

	var halts []TradeHalt

	for i, record := range records {
		if i == 0 {
			continue
		}
		if len(record) != 8 {
			panic("malformed record")
		}
		halts = append(halts, TradeHalt{
			HaltDate:   record[0],
			HaltTime:   record[1],
			Symbol:     record[2],
			Name:       record[3],
			Exchange:   record[4],
			Reason:     record[5],
			ResumeDate: record[6],
			NYSETime:   record[7],
		})
	}

	return halts, nil
}

func main() {
	var cli CLI
	ctx := kong.Parse(&cli)
	ctx.FatalIfErrorf(ctx.Run())
}
