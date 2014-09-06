package main

// +build version_embedded

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/r7kamura/entoverse"
	"github.com/samuel/go-gettext/gettext"
)

const (
	trace = debugT(false)

	majorVersion = "0.0"
	maxGoroutine = 100

	hostPatern = "^((?P<commit>[0-9a-f]{7})\\.)?(?P<refspec>[^\\.]+)\\.(?P<repository_name>[^\\.]+)\\.(?P<repository_owner>[^\\.]+)\\.(?P<repository_service>.+)\\.(?P<moorage_server>moorage):(?P<moorage_server_port>[0-9]+)$"
	runCommand = `
cd /opt/src;
if ! (
  git clone --branch ${refspec} --single-branch --depth=1 https://${repository_service}/${repository_owner}/${repository_name}.git ${repository_service}/${repository_owner}/${repository_name}
 ); then
 ( cd ${repository_service}/${repository_owner}/${repository_name} &&
   git fetch --depth=1 --force origin ${refspec} &&
   git checkout --force origin/${refspec}
 )
fi;
docker build --tag=${repository_service}/${repository_owner}/${repository_name}:${refspec} $(pwd)/${repository_service}/${repository_owner}/${repository_name} &&
docker run --detach --publish-all --name=${refspec}.${repository_name}.${repository_owner}.${repository_service} ${repository_service}/${repository_owner}/${repository_name}:${refspec}
`
	inspectCommand = `
docker inspect --format="{{.NetworkSettings.IPAddress}}{{range \$$i, \$$e := .NetworkSettings.Ports}}:{{\$$i}}{{end}}" ${refspec}.${repository_name}.${repository_owner}.${repository_service}
`
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
	reHostPatern       = regexp.MustCompile(hostPatern)
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

	hostConverter := func(originalHost string) string {

		develop.Println(originalHost)
		originalHost = "master.docker-http-server-hello-world.MiCHiLU.github.com.moorage:3000"
		//originalHost = "6d211fa.docker.timecard-rails.MiCHiLU.github.com.moorage:3000"
		originalHost = strings.ToLower(originalHost)

		if !reHostPatern.MatchString(originalHost) {
			//	return
		}

		//develop.Println(reHostPatern.ReplaceAllString(originalHost, runCommand))
		cmd := exec.Command("sh", "-c", reHostPatern.ReplaceAllString(originalHost, runCommand))
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err := cmd.Run()
		if err != nil {
			log.Print(err)
		}

		cmd = exec.Command("sh", "-c", reHostPatern.ReplaceAllString(originalHost, inspectCommand))
		var out bytes.Buffer
		cmd.Stdout = &out
		err = cmd.Run()
		if err != nil {
			log.Print(err)
		}
		var output string
		output = out.String()
		develop.Println(reHostPatern.ReplaceAllString(originalHost, inspectCommand))
		develop.Println(out)
		var ip_port []string
		ip_port = strings.Split(output, ":")
		var ip string
		ip = ip_port[0]
		var port int
		var port_another int
		var protocol string
		for _, port_protocol_line := range ip_port[1:] {
			var port_protocol []string
			port_protocol = strings.SplitN(port_protocol_line, "/", 2)
			protocol = port_protocol[1]
			if port == 0 {
				port, err = strconv.Atoi(port_protocol[0])
			} else if protocol != "udp" {
				port_another, err = strconv.Atoi(port_protocol[0])
				if port > port_another {
					port = port_another
				}
			}
		}

		develop.Println(fmt.Sprintf("%s:%d", ip, port))
		if ip != "" && port != 0 {
			return fmt.Sprintf("%s:%d", ip, port)
		}
		return ""
	}

	// Creates an entoverse.Proxy object as an HTTP handler.
	proxy := entoverse.NewProxyWithHostConverter(hostConverter)

	// Runs a reverse-proxy server on http://localhost:3000/
	http.ListenAndServe(":3000", proxy)

	if err != nil {
		develop.Println(err)
		log.Print(err)
		return
	}
}
