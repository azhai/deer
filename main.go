package main

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/sanity-io/litter"
	"tinygo.org/x/go-llvm"
)

var (
	dumpIR   = flag.Bool("d", false, "dump the llvm ir")
	execProg = flag.Bool("e", false, "evaluate the code")
	optimize = flag.Int("O", 0, "the level of optimization")
	output   = flag.String("o", "", "output filename")
	verbose  = flag.Bool("v", false, "verbose output")
	writer   io.Writer
)

var dumpConfig = litter.Options{
	HomePackage:       "main",
	StripPackageNames: true,
	HidePrivateFields: false,
	Separator:         "\n",
}

func main() {
	flag.Parse()
	files := flag.Args()
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: deer [options] <input files>")
		flag.PrintDefaults()
		os.Exit(1)
	}

	logLevel := slog.LevelError
	if *verbose {
		logLevel = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel,
	})))

	initExecutionEngine()

	needWrite, filename, extname := true, "", ""
	if *output == "" {
		if *dumpIR || *execProg {
			needWrite = false
			writer = os.Stdout
		} else {
			filename = filepath.Base(files[0])
			pos := len(filename) - len(filepath.Ext(filename))
			*output = filename[:pos]
		}
	}
	filename = *output
	var outFile *os.File
	if needWrite {
		extname = strings.ToLower(filepath.Ext(*output))
		if extname == "" {
			filename += ".o"
		}
		var err error
		outFile, err = os.Create(filename)
		handleError(true, "can not open the file:", err)
		defer outFile.Close()
		writer = outFile
	}

	lex := Lex()
	go func() {
		for _, fn := range files {
			f, err := os.Open(fn)
			handleError(true, "", err)
			lex.Add(f)
		}
		lex.Done()
	}()

	tokens := lex.Tokens()
	if extname == ".tok" {
		for tok := range tokens {
			fmt.Fprintf(writer, "%s: %s\n", tok.pos, tok)
		}
		if needWrite {
			return
		}
	}

	nodes := Parse(tokens)
	if *dumpIR {
		EmitIR(nodes)
	}
	if *execProg {
		Exec(nodes)
	}
	if !needWrite {
		return
	}

	switch extname {
	case ".ast":
		for nod := range nodes {
			fmt.Fprintln(writer, dumpConfig.Sdump(nod))
		}
	case ".bc":
		VisitNodes(nodes, nil)
		if *optimize > 0 {
			Optimize()
		}
		llvm.WriteBitcodeToFile(rootModule, outFile)
	default:
		obj, err := Compile(nodes, rootModule)
		handleError(true, "can not emit object file to memory buffer:", err)
		_, err = outFile.Write(obj)
		handleError(true, "write to file failure:", err)
		if extname == "" {
			args := []string{"-o", *output, "lib.c", filename}
			if runtime.GOOS != "darwin" {
				args = append(args, "-lm")
			}
			cmd := exec.Command("clang", args...)
			slog.Debug("linking", "cmd", cmd.String())
			handleError(true, "build failure:", cmd.Run())
			os.Chmod(*output, 0755)
		}
	}
}

func handleError(isExit bool, msg string, err error) {
	if err == nil {
		return
	}
	if len(msg) > 0 {
		fmt.Fprintln(os.Stderr, msg)
	}
	fmt.Fprintln(os.Stderr, err)
	if isExit {
		os.Exit(1)
	}
}
