package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/marcospjr07/docktor-pt-br/internal/check"
	"github.com/marcospjr07/docktor-pt-br/internal/linux"
	"github.com/marcospjr07/docktor-pt-br/internal/reporter"
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr, scan))
}

func scan(ctx context.Context) check.Report {
	return check.NewRunner(linux.Checks()...).Run(ctx)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer, scanFn func(context.Context) check.Report) int {
	if len(args) == 0 {
		writeHelp(stdout)
		return 0
	}

	switch args[0] {
	case "--help", "-h", "help":
		if len(args) != 1 {
			fmt.Fprintf(stderr, "docktor: argumento inesperado %q\n", args[1])
			writeHelp(stderr)
			return 2
		}
		writeHelp(stdout)
		return 0
	case "scan":
		jsonOutput, help := false, false
		for _, arg := range args[1:] {
			switch {
			case arg == "--json" && !jsonOutput:
				jsonOutput = true
			case (arg == "--help" || arg == "-h") && !help:
				help = true
			default:
				fmt.Fprintf(stderr, "docktor scan: argumento inesperado %q\n", arg)
				writeScanHelp(stderr)
				return 2
			}
		}
		if help {
			writeScanHelp(stdout)
			return 0
		}
		report := scanFn(ctx)
		writeReport := reporter.WriteTerminal
		if jsonOutput {
			writeReport = reporter.WriteJSON
		}
		if err := writeReport(stdout, report); err != nil {
			fmt.Fprintf(stderr, "docktor: não foi possível escrever o relatório: %v\n", err)
			return 1
		}
		return 0
	default:
		fmt.Fprintf(stderr, "docktor: comando desconhecido %q\n", args[0])
		writeHelp(stderr)
		return 2
	}
}

func writeHelp(w io.Writer) {
	fmt.Fprint(w, "O Docktor lê indicadores de integridade de servidores Linux.\n\nUso:\n  docktor scan [--json]\n  docktor --help\n\nComandos:\n  scan    Executa diagnósticos de integridade somente leitura\n")
}

func writeScanHelp(w io.Writer) {
	fmt.Fprint(w, "Uso: docktor scan [--json]\n\nExecuta diagnósticos de integridade somente leitura no Linux e exibe um resumo.\n\nOpções:\n  --json  Escreve um relatório JSON em vez da saída de terminal\n  --help  Exibe a ajuda do comando\n")
}
