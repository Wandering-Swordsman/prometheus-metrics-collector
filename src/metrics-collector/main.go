package main

import (
	"fmt"
	"io"
	"bufio"
	"encoding/json"
	"regexp"
	"net/http"
	"bytes"

	"gopkg.in/alecthomas/kingpin.v2"
)

var (
	logFlag = kingpin.Flag("log", "Write logs to STDERR").Bool()

	inFileFlag = kingpin.Flag("json", "Read in a .json file.").Required().PlaceHolder("file_name").File()
	deleteOldFlag = kingpin.Flag("delete-old", "Delete old, repeated scrapes in the event of a server cut").Bool()
	pushLabelCommand = kingpin.Command("push-label", "Add name-value pairs to push names in the form <name>=<value>")
	pushLabelArgs = pushLabelCommand.Arg("push-label-args", "push arguments").Strings()
	machineLabelFlag = kingpin.Flag("machine-label", "Specify the machine label").Required().PlaceHolder("machine_label").String()
	pushURLFlag = kingpin.Flag("push-url", "Specify the url to read from").Required().PlaceHolder("url").String()
	readPathFlags = kingpin.Flag("read-path", "specify the paths to read from (include a leading forward slash)").Required().PlaceHolder("read_path").Strings()

	labelFlagArgs = kingpin.Flag("add-label", "Add a label and value in the form <label>=<value>.").PlaceHolder("<label>=<value>").Short('a').StringMap()
	dropFlagArgs = kingpin.Flag("drop-metric", "Drop a metric").PlaceHolder("some_metric").Short('d').Strings()
	inFileFlagArg = kingpin.Flag("in", "Read in a file").PlaceHolder("file_name").File();
	outFileFlagArg = kingpin.Flag("out", "Write to a File").PlaceHolder("file_name").String(); //string because has to create the file
	defaultDropFlag = kingpin.Flag("drop-default", "Drop default metrics").Bool();
	inDirFlagArg = kingpin.Flag("in-dir", "Read in a directory").PlaceHolder("dir_name").String();
)

//struct that holds a label, its associated value, and a float value. Used for adding metrics

func main() {
	kingpin.Parse()

	//essentially a parser
	dec := json.NewDecoder(bufio.NewReader(*inFileFlag))

	//set up structs for the parser
	type Tunnel struct {
		ProtoType string `json:"type"`
		User string `json:"user"`
		Port string `json:"port"`
	}

	type Master struct {
		Host string `json:"host"`
		Tunnels []Tunnel `json:"tunnels"`
		Description string `json:"description"`
		Name string `json:"name"`
		ID string `json:"id"`
	}

	type Machine struct {
		Master Master `json:"master"`
	}

	// ignore open bracket
	_, err := dec.Token()
	if err != nil {
		logFatalf("Error getting first token: %s", err)
	}

	//set up push path without machine/name
	var pushPathStr string
	for _, elem := range *pushLabelArgs {
		key, value, err := kvParse(elem)
		if err != nil {
			logPrintln(err)
		}
		pushPathStr = fmt.Sprintf("%s/%s/%s", pushPathStr, key, value)
	}

	logPrintln("Starting collection from all devices\n")

	//if there are more elements in the array, keep going
	for dec.More(){
		var rStruct Relabeler
		var machine Machine

		//creating a new metric family for the device
		currFam := rStruct.newGaugeMetricFamily("metrics_collector_target_up", "1 if target is up, 0 if target is down")

		//parse with decoder
		if err := dec.Decode(&machine); err == io.EOF {
			break
		} else if err != nil {
			logFatal(err)
		}

		//find relevant fields for command line args
		host := machine.Master.Host
		name := machine.Master.Name

		//fullPushPathStr including machine/name
		fullPushPathStr := fmt.Sprintf("%s%s/%s/%s", *pushURLFlag, pushPathStr, *machineLabelFlag, name)

		//find the http port
		var httpIdx int
		for idx, master := range machine.Master.Tunnels {
			if string(master.ProtoType) == "http"{
					httpIdx = idx
					break
			}
		}
		port := machine.Master.Tunnels[httpIdx].Port

		logPrintf("Starting collection from %s...\n", name)

		if *deleteOldFlag {
			deletePath(fullPushPathStr)
		}

		for _, path := range *readPathFlags {
			//add a new metric that says if the device is on or on while performing the get command
			hostStr := fmt.Sprintf("http://%s:%s%s", host, port, path)

			logPrintf("Attempting GET from %s\n", hostStr)
			getResp, err := http.Get(hostStr)

			//404 returns a nil error
			if err != nil || getResp.StatusCode == 404 {
				if err != nil {
					logPrintf("Failure: %s. Continuing...\n", err)
				} else {
					logPrint("Failure: 404 Not Found. Continuing...\n")
				}
				rStruct.setGetSuccess(false)
				addGaugeMetrics(currFam, LabelValueFloat{
					Label: "path",
					Value: path,
					Float: 0,
				})
			} else {
				logPrintln("Success")
				rStruct.setGetSuccess(true)
				addGaugeMetrics(currFam, LabelValueFloat{
					Label: "path",
					Value: path,
					Float: 1,
				})
				//relabels and then sets OutBytes in rStruct to the byte array of the output
				logPrintln("Relabeling metrics...")
				rStruct.relabel(getResp.Body)
				logPrintln("Relabeling complete")
			}

			logPrintf("Attempting POST to %s\n", fullPushPathStr)
			_, err = http.Post(fullPushPathStr, "application/octet-stream", bytes.NewReader(rStruct.OutBytes))
			if err != nil {
        logPrintf("Failure: %s\n", err)
    	} else{
				logPrintln("Success")
			}
		}
		logPrintf("Collection from %s complete\n\n", name)
	}

	// ignore closing bracket
	_, err = dec.Token()
	if err != nil {
		logFatal(err)
	}

	logPrintf("Collection from all devices complete")
}

//parse key=value pair
func kvParse(str string) (string, string, error) {
	parts := regexp.MustCompile("=").Split(str, 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("function kvParse() error: Expected KEY=VALUE got %s", str)
	}
	return parts[0], parts[1], nil
}

//delete the old scraped stuff in the event of a server cut
func deletePath(path string) {
	logPrintf("Deleting old metrics from %s\n", path)
  client := &http.Client{}

    // Create request
  req, err := http.NewRequest("DELETE", path, nil)
  if err != nil {
      logPrintf("Failure: %s. Continuing...\n", err)
  }

  // Fetch Request
  resp, err := client.Do(req)
  if err != nil {
      logPrintf("Failure: %s. Continuing\n", err)
  }

	logPrintln("Success")

  defer resp.Body.Close()
}
