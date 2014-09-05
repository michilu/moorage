package main

// +build version_embedded

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/samuel/go-gettext/gettext"
	"go-scaffold/locale/ja"
)

const (
	trace = debugT(false)

	majorVersion = "0.0"
	maxGoroutine = 100
)

type debugT bool

func (d debugT) Println(args ...interface{}) {
	if d {
		_, file, line, ok := runtime.Caller(1)
		if ok {
			if line > maxLineNumber {
				maxLineNumber = line
				maxLineDigits = len(fmt.Sprint(maxLineNumber))
			}
			args = append([]interface{}{
				file,
				fmt.Sprintf(fmt.Sprintf("%%%dd:", maxLineDigits), line),
			}, args...)
		}
		log.Println(args...)
	}
}

func (d debugT) Printf(format string, args ...interface{}) {
	if d {
		_, file, line, ok := runtime.Caller(1)
		if ok {
			if line > maxLineNumber {
				maxLineNumber = line
				maxLineDigits = len(fmt.Sprint(maxLineNumber))
			}
			args = []interface{}{
				file,
				fmt.Sprintf(fmt.Sprintf("%%%dd:", maxLineDigits), line),
				fmt.Sprintf(format, args...),
			}
			log.Println(args...)
		} else {
			log.Printf(format, args...)
		}
	}
}

func (d debugT) Do(f func()) {
	if d {
		f()
	}
}

var (
	buildAt            string
	catalog            *gettext.Catalog
	debug              bool
	develop            debugT
	interval           int
	maxLineDigits      int
	maxLineNumber      = 0
	maxProcesses       = runtime.NumCPU()
	memStats           runtime.MemStats
	minorVersion       string
	name               string
	semaphoreFile      chan int
	semaphoreFileCount = maxProcesses * 2 * 2
	semaphoreHTTP      chan int
	semaphoreHTTPCount = maxProcesses * 2 * 2
	version            string
	waitWG             bool
	wg                 sync.WaitGroup
	wgFile             sync.WaitGroup
)

func init() {
	runtime.GOMAXPROCS(maxProcesses)

	if semaphoreFileCount > 8 {
		semaphoreFileCount = 8
	}
	if semaphoreHTTPCount > 8 {
		semaphoreHTTPCount = 8
	}
	semaphoreFile = make(chan int, semaphoreFileCount)
	semaphoreHTTP = make(chan int, semaphoreHTTPCount)

	catalog, err := getCatalog()

	flag.BoolVar(&debug, "v", false, catalog.GetText("print debug messages"))
	flag.IntVar(&interval, "i", 0, catalog.GetText("interval"))
	flag.Usage = func() {
		fmt.Printf("Usage: %s [options]\n\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	develop = debugT(debug)

	if err != nil {
		develop.Println(err)
	}
	if catalog == nil || catalog == gettext.NullCatalog {
		develop.Do(func() {
			develop.Println("Failed at GetCatalog.", "LANGUAGE: ", getLANGUAGE())
		})
	}
}

func main() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		for sig := range c {
			develop.Println("os.Signal:", sig)
			wgFile.Wait()
			os.Exit(0)
		}
	}()

	process()
	if interval > 0 {
		for {
			time.Sleep(time.Duration(interval) * time.Second)
			process()
		}
	}
}

func getCatalog() (result *gettext.Catalog, err error) {
	if catalog != nil {
		result = catalog
		return
	}

	var mo []byte

	switch getLANGUAGE() {
	case "ja":
		mo = ja.Mo()
	}
	if mo != nil {
		catalog, err = gettext.ParseMO(bytes.NewReader(mo))
	} else {
		catalog = gettext.NullCatalog
	}
	result = catalog
	return
}

func getLANGUAGE() (lang string) {
	//refs: http://www.gnu.org/software/gettext/manual/html_node/Locale-Environment-Variables.html
	for _, key := range []string{"LANGUAGE", "LC_ALL", "LC_MESSAGES", "LANG"} {
		lang = os.Getenv(key)
		if lang != "" {
			lang = strings.SplitN(lang, "_", 2)[0]
			break
		}
	}
	return
}

func AddWaitGroup(f func()) {
	trace.Println()
	if waitWG == true {
		trace.Println()
		waitWG = false
		for {
			numGoroutine := runtime.NumGoroutine()
			if numGoroutine < maxGoroutine {
				break
			}
			develop.Println(
				"NumGoroutine:", numGoroutine,
				"semaphoreFile:", len(semaphoreFile),
				"semaphoreHTTP:", len(semaphoreHTTP),
			)
			trace.Println()
			runtime.Gosched()
			trace.Println()
		}
	} else {
		trace.Println()
		if rand.Intn(10) == 0 {
			numGoroutine := runtime.NumGoroutine()
			if numGoroutine > maxGoroutine {
				runtime.ReadMemStats(&memStats)
				develop.Println(
					"Alloc:", memStats.Alloc,
					"NumGC:", memStats.NumGC,
					"NumGoroutine:", numGoroutine,
					"semaphoreFile:", len(semaphoreFile),
					"semaphoreHTTP:", len(semaphoreHTTP),
				)
				waitWG = true
			}
		}
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		f()
	}()
}

func process() {
	start := time.Now()
	fmt.Printf("%v %v.%v (%v, %v, %v/%v)\n", name, majorVersion, minorVersion, version, buildAt, runtime.GOOS, runtime.GOARCH)
	defer func() {
		wg.Wait()
		develop.Do(func() {
			runtime.ReadMemStats(&memStats)
			develop.Println(memStats.Alloc, memStats.NumGC)
		})
		log.Println("Finished:", time.Now().Sub(start))
	}()

	var err error

	AddWaitGroup(func() {})

	if err != nil {
		develop.Println(err)
		log.Print(err)
		return
	}
}
