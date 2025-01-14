package main

import (
	"fmt"
	"github.com/gorilla/websocket"
	"github.com/zairza-cetb/bench-routes/src/lib/filters"
	"github.com/zairza-cetb/bench-routes/src/lib/utils"
	"github.com/zairza-cetb/bench-routes/src/lib/utils/parser"
	"github.com/zairza-cetb/bench-routes/tsdb"
	"io"
	"log"
	"net/http"
	"os"
	"os/user"
	"strconv"
	"time"
)

var (
	port     = ":9090"
	upgrader = websocket.Upgrader{
		// set buffer to 4 mega-bytes size
		ReadBufferSize:  4048,
		WriteBufferSize: 4048,
	}
	configuration parser.YAMLBenchRoutesType
)

const (
	logFilePrefix = "bench-route-"
	logDirectory  = "br-logs"
)

func init() {
	setupLogger()
	log.Printf("initializing bench-routes ...")

	// load configuration file
	configuration.Address = utils.ConfigurationFilePath
	configuration = *configuration.Load()

	var ConfigURLs []string

	// Load and build TSDB chain
	// searching for unique URLs
	for _, r := range configuration.Config.Routes {
		found := false
		for _, i := range ConfigURLs {
			if i == r.URL {
				found = true
				break
			}
		}
		if !found {
			filters.HTTPPingFilter(&r.URL)
			ConfigURLs = append(ConfigURLs, r.URL)
			tsdb.PingDBNames[r.URL] = utils.GetHash(r.URL)
			tsdb.FloodPingDBNames[r.URL] = utils.GetHash(r.URL)
		}
	}
	// forming ping chain
	for i, v := range ConfigURLs {
		path := utils.PathPing + "/" + "chunk_ping_" + v + ".json"
		inst := &tsdb.ChainPing{
			Path:           path,
			Chain:          []tsdb.BlockPing{},
			LengthElements: 0,
			Size:           0,
		}
		// Initiate the chain
		tsdb.GlobalPingChain = append(tsdb.GlobalPingChain, inst)
		tsdb.GlobalPingChain[i] = tsdb.GlobalPingChain[i].InitPing()
		tsdb.GlobalPingChain[i].SavePing()
	}

	// forming ping chain
	for i, v := range ConfigURLs {
		path := utils.PathFloodPing + "/" + "chunk_flood_ping_" + v + ".json"
		inst := &tsdb.ChainFloodPing{
			Path:           path,
			Chain:          []tsdb.BlockFloodPing{},
			LengthElements: 0,
			Size:           0,
		}
		// Initiate the chain
		tsdb.GlobalFloodPingChain = append(tsdb.GlobalFloodPingChain, inst)
		tsdb.GlobalFloodPingChain[i] = tsdb.GlobalFloodPingChain[i].InitFloodPing()
		tsdb.GlobalFloodPingChain[i].SaveFloodPing()
	}

	for i, v := range ConfigURLs {
		path := utils.PathJitter + "/" + "chunk_jitter_" + v + ".json"
		inst := &tsdb.Chain{
			Path:           path,
			Chain:          []tsdb.Block{},
			LengthElements: 0,
			Size:           0,
		}
		// Initiate the chain
		tsdb.GlobalChain = append(tsdb.GlobalChain, inst)
		tsdb.GlobalChain[i] = tsdb.GlobalChain[i].Init()
		tsdb.GlobalChain[i].Save()
	}

	// forming req-res-delay chain
	for i, route := range configuration.Config.Routes {
		path := utils.PathReqResDelayMonitoring + "/" + "chunk_req_res_" + filters.RouteDestroyer(route.URL)
		// Create sample chains to init in each TSDB file
		sampleResponseDelay := &tsdb.Chain{
			Path:           path + "_delay.json",
			Chain:          []tsdb.Block{},
			LengthElements: 0,
			Size:           0,
		}
		sampleResponseLength := &tsdb.Chain{
			Path:           path + "_length.json",
			Chain:          []tsdb.Block{},
			LengthElements: 0,
			Size:           0,
		}
		sampleResponseStatusCode := &tsdb.Chain{
			Path:           path + "_status.json",
			Chain:          []tsdb.Block{},
			LengthElements: 0,
			Size:           0,
		}
		tsdb.GlobalResponseDelay = append(tsdb.GlobalResponseDelay, sampleResponseDelay)
		tsdb.GlobalResponseLength = append(tsdb.GlobalResponseLength, sampleResponseLength)
		tsdb.GlobalResponseStatusCode = append(tsdb.GlobalResponseStatusCode, sampleResponseStatusCode)

		// Initiate all chains
		tsdb.GlobalResponseDelay[i] = tsdb.GlobalResponseDelay[i].Init()
		tsdb.GlobalResponseLength[i] = tsdb.GlobalResponseLength[i].Init()
		tsdb.GlobalResponseStatusCode[i] = tsdb.GlobalResponseStatusCode[i].Init()

		// Commit all chains to the TSDB
		tsdb.GlobalResponseDelay[i].Save()
		tsdb.GlobalResponseLength[i].Save()
		tsdb.GlobalResponseStatusCode[i].Save()
	}

	// keep the below line to the end of file so that we ensure that we give a confirmation message only when all the
	// required resources for the application is up and healthy
	log.Printf("Bench-routes is up and running\n")
}

func main() {

	if len(os.Args) > 1 {
		port = ":" + os.Args[1]
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("ping from %s, sent pong in response\n", r.RemoteAddr)
	})
	http.HandleFunc("/websocket", func(w http.ResponseWriter, r *http.Request) {
		upgrader.CheckOrigin = func(r *http.Request) bool { return true }
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Fatalf("error using upgrader %s\n", err)
		}
		// capture client request for enabling series of responses unless its killed
		for {
			messageType, message, err := ws.ReadMessage()
			if err != nil {
				log.Printf("connection to client lost.\n%s\n", err)
				return
			}
			messageStr := string(message)
			log.Printf("type: %d\n message: %s \n", messageType, messageStr)

			// generate appropriate signals from incoming messages
			switch messageStr {
			// ping
			case "force-start-ping":
				// true if success else false
				// e := ws.WriteMessage(1, []byte(strconv.FormatBool(controllers.PingController("start"))))
				e := ws.WriteMessage(1, []byte(strconv.FormatBool(HandlerPingGeneral("start"))))
				if e != nil {
					panic(e)
				}
			case "force-stop-ping":
				e := ws.WriteMessage(1, []byte(strconv.FormatBool(HandlerPingGeneral("stop"))))
				if e != nil {
					panic(e)
				}

				// flood-ping
			case "force-start-flood-ping":
				e := ws.WriteMessage(1, []byte(strconv.FormatBool(HandlerFloodPingGeneral("start"))))
				if e != nil {
					panic(e)
				}
			case "force-stop-flood-ping":
				e := ws.WriteMessage(1, []byte(strconv.FormatBool(HandlerFloodPingGeneral("stop"))))
				if e != nil {
					panic(e)
				}

				// jitter
			case "force-start-jitter":
				e := ws.WriteMessage(1, []byte(strconv.FormatBool(HandlerJitterGeneral("start"))))
				if e != nil {
					panic(e)
				}
			case "force-stop-jitter":
				e := ws.WriteMessage(1, []byte(strconv.FormatBool(HandlerJitterGeneral("stop"))))
				if e != nil {
					panic(e)
				}

				// request-response-monitoring
			case "force-start-req-res-monitoring":
				e := ws.WriteMessage(1, []byte(strconv.FormatBool(HandleReqResGeneral("start"))))
				if e != nil {
					panic(e)
				}
			case "force-stop-req-res-monitoring":
				e := ws.WriteMessage(1, []byte(strconv.FormatBool(HandleReqResGeneral("stop"))))
				if e != nil {
					panic(e)
				}
			}
		}
	})

	// launch service
	log.Fatal(http.ListenAndServe(port, nil))

}

func setupLogger() {
	currTime := time.Now()
	currFileName := fmt.Sprint(logFilePrefix, currTime.Format("2006-01-02#15:04:05"), ".log")
	user, err := user.Current()
	if err != nil {
		fmt.Printf("cannot access current user data\n")
		return
	}

	homePath := user.HomeDir
	logDirectoryPath := homePath + "/" + logDirectory
	err = os.MkdirAll(logDirectoryPath, 0755)
	if err != nil {
		fmt.Printf("error creating log directory : %s\n", logDirectoryPath)
		return
	}
	logFilePath := logDirectoryPath + "/" + currFileName
	file, err := os.OpenFile(logFilePath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0444)
	if err != nil {
		fmt.Printf("error opening log file : %s\n", logFilePath)
		return
	}
	writer := io.MultiWriter(os.Stdout, file)
	log.SetOutput(writer)
	log.SetPrefix("LOG: ")
	log.SetFlags(log.Ldate | log.Lmicroseconds | log.Lshortfile)
}
