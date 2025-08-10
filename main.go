package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/alecthomas/kong"
)

const (
	nyseTradeHaltURL = "https://www.nyse.com/api/trade-halts/current/download"
	bellSound        = "\a"
)

var nyseLocation *time.Location

func init() {
	var err error
	nyseLocation, err = time.LoadLocation("America/New_York")
	if err != nil {
		panic(err)
	}
}

type CLI struct {
	Fetch FetchCmd `cmd:"" help:"Fetch current NYSE trade halts."`
	Watch WatchCmd `cmd:"" help:"Watch for new NYSE trade halts and ding on new halts."`
}

type FetchCmd struct{}

func (f *FetchCmd) Run() error {
	halts, _, err := fetchTradeHalts()
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
	displayFunc := func(halts []TradeHalt, lastModified *time.Time) {
		clearScreen()
		displayHaltsTable(halts)
		fmt.Println()

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', 0)
		fmt.Fprintf(w, "Last fetch\t@ %s\n", time.Now().Format(time.RFC1123Z))
		fmt.Fprintf(w, "Last updated\t@ %s\n", lastModified.Local().Format(time.RFC1123Z))
		w.Flush()
	}

	prevHalts := make(map[string]TradeHalt)

	ticker := time.NewTicker(w.Interval)
	defer ticker.Stop()

	for {
		currentHalts, lastModified, err := fetchTradeHalts()
		if err != nil {
			log.Fatal(err)
		}

		haltsUpdated := false

		for _, halt := range currentHalts {
			prevHalt, ok := prevHalts[halt.Symbol]
			if ok {
				if prevHalt.ResumeDateTime != halt.ResumeDateTime {
					// Resume time updated
					prevHalts[halt.Symbol] = halt
					haltsUpdated = true
				}

				continue
			}

			// New halt added
			prevHalts[halt.Symbol] = halt
			haltsUpdated = true
		}

		if haltsUpdated {
			fmt.Print(bellSound)
		}

		displayFunc(currentHalts, lastModified)

		if _, ok := <-ticker.C; !ok {
			break
		}
	}

	return nil
}

func displayHaltsTable(halts []TradeHalt) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SYMBOL\tNAME\tEXCHANGE\tREASON\tHALT TIME (LOCAL)\tRESUME TIME (LOCAL)")
	fmt.Fprintln(w, "------\t----\t--------\t------\t-----------------\t-------------------")

	for _, halt := range halts {
		haltTimeLocal := ""
		if !halt.HaltDateTime.IsZero() {
			haltTimeLocal = halt.HaltDateTime.Local().Format("2006-01-02 15:04:05")
		}

		resumeTimeLocal := ""
		if !halt.ResumeDateTime.IsZero() {
			resumeTimeLocal = halt.ResumeDateTime.Local().Format("2006-01-02 15:04:05")
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			halt.Symbol, halt.Name, halt.Exchange, halt.Reason,
			haltTimeLocal, resumeTimeLocal)
	}
	w.Flush()
}

func clearScreen() {
	fmt.Print("\033[2J\033[H")
}

func tryUnquote(s string) string {
	unquoted, err := strconv.Unquote(s)
	if err != nil {
		return s
	}
	return unquoted
}

type TradeHalt struct {
	Symbol         string
	Name           string
	Exchange       string
	Reason         string
	HaltDateTime   time.Time
	ResumeDateTime time.Time
}

func parseTradeHalts(reader io.Reader) ([]TradeHalt, error) {
	csvReader := csv.NewReader(reader)
	records, err := csvReader.ReadAll()
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

		var haltDateTime time.Time
		if record[0] != "" && record[1] != "" {
			haltDateTime, err = time.ParseInLocation("2006-01-02 15:04:05", record[0]+" "+record[1], nyseLocation)
			if err != nil {
				log.Printf("failed to parse halt datetime for %s: %v", record[2], err)
			}
		}

		var resumeDateTime time.Time
		if record[6] != "" && record[7] != "" {
			resumeDateTime, err = time.ParseInLocation("2006-01-02 15:04:05", record[6]+" "+record[7], nyseLocation)
			if err != nil {
				log.Printf("failed to parse resume datetime for %s: %v", record[2], err)
			}
		}

		halts = append(halts, TradeHalt{
			Symbol:         record[2],
			Name:           tryUnquote(record[3]),
			Exchange:       record[4],
			Reason:         record[5],
			HaltDateTime:   haltDateTime,
			ResumeDateTime: resumeDateTime,
		})
	}

	return halts, nil
}

func fetchTradeHalts() ([]TradeHalt, *time.Time, error) {
	resp, err := http.Get(nyseTradeHaltURL)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch trade halts: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("bad status code: %d", resp.StatusCode)
	}

	halts, err := parseTradeHalts(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse trade halts: %w", err)
	}

	lastModified, err := time.Parse(time.RFC1123, resp.Header.Get("Last-Modified"))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse last modified header: %w", err)
	}

	return halts, &lastModified, nil
}

func main() {
	var cli CLI
	ctx := kong.Parse(&cli)
	ctx.FatalIfErrorf(ctx.Run())
}
