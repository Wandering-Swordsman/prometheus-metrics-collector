package main

import (
	"fmt"
	"io"
	"bufio"
	"encoding/json"
	"log"
	"regexp"
	"net/http"
	"bytes"

	"gopkg.in/alecthomas/kingpin.v2"
	"github.com/prometheus/client_golang/prometheus"
)

//struct to hold the resposne from the get function
type HTTPResponse struct {
	GetResp http.Response
}

var (
	inFileFlag = kingpin.Flag("json", "Read in a .json file.").Required().PlaceHolder("file_name").File()
	deleteOldFlag = kingpin.Flag("delete-old", "Delete old, repeated scrapes in the event of a server cut").Bool()
	pushLabelCommand = kingpin.Command("push-label", "Add name-value pairs to push names in the form <name>=<value>")
	pushLabelArgs = pushLabelCommand.Arg("push-label-args", "push arguments").Strings()
	machineLabelFlag = kingpin.Flag("machine-label", "Specify the machine label").Required().PlaceHolder("machine_label").String()
	pushURLFlag = kingpin.Flag("push-url", "Specify the url to read from").Required().PlaceHolder("url").String()
	readPathFlags = kingpin.Flag("read-path", "specify the paths to read from (include a leading forward slash)").Required().PlaceHolder("read_path").Strings()

	relabelLabelFlagArgs = kingpin.Flag("add-label", "Add a label and value in the form <label>=<value>.").PlaceHolder("<label>=<value>").Short('a').StringMap()
	relabelDropFlagArgs = kingpin.Flag("drop-metric", "Drop a metric").PlaceHolder("some_metric").Short('d').Strings()
	relabelInFileFlagArg = kingpin.Flag("in", "Read in a file").PlaceHolder("file_name").File();
	relabelOutFileFlagArg = kingpin.Flag("out", "Write to a File").PlaceHolder("file_name").String(); //string because has to create the file
	relabelDefaultDropFlag = kingpin.Flag("drop-default", "Drop default metrics").Bool();
	relabelInDirFlagArg = kingpin.Flag("in-dir", "Read in a directory").PlaceHolder("dir_name").String();
)

func main() {
	kingpin.Parse()

	//essentially a parser
	dec := json.NewDecoder(bufio.NewReader(*inFileFlag))

	var rStruct Relabeler
	var response HTTPResponse

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
		ID int `json:"id"`
	}

	type Machine struct {
		Master Master `json:"master"`
	}

	// ignore open bracket
	_, err := dec.Token()
	if err != nil {
		log.Fatal(err)
	}

	//set up push path without machine/name
	var pushPathStr string
	for _, elem := range *pushLabelArgs {
		key, value, err := kvParse(elem)
		if err != nil {
			log.Fatal(err)
		}
		pushPathStr = fmt.Sprintf("%s/%s/%s", pushPathStr, key, value)
	}

	//if there are more elements in the array, keep going
	for dec.More(){
		var machine Machine

		//parse with decoder
		if err := dec.Decode(&machine); err == io.EOF {
			break
		} else if err != nil {
			log.Fatal(err)
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

		if *deleteOldFlag {
			deletePath(fullPushPathStr)
		}

		for _, path := range *readPathFlags {
			//add a new metric that says if the device is on or on while performing the get command
			hostStr := fmt.Sprintf("http://%s:%s%s", host, port, path)
			prometheus.Register(prometheus.NewGaugeFunc(
				prometheus.GaugeOpts{
					Name:      "metricscollector_target_up",
					Help:      "1 if device is up, 0 if it is not.",
				},
				func() float64 { //get command
						err := response.getResp(hostStr)
						if err != nil { return 1 }
						return 0
					},
				))
					//TODO: create a new metric saying the thingy is off
					//relabeler.something that adds metric
					//metricscollector_target_up{machine="sz-stubbe1"} 0

			//relabels and then sets OutBytes in rStruct to the byte array of the output
			rStruct.relabel(relabelLabelFlagArgs, relabelDropFlagArgs, relabelInFileFlagArg, relabelOutFileFlagArg, relabelDefaultDropFlag, relabelInDirFlagArg, response.GetResp.Body)
			_, err = http.Post(fullPushPathStr, "application/octet-stream", bytes.NewReader(rStruct.OutBytes))
			if err != nil {
        	fmt.Printf("%s\n", err)
    	}
		}
	}

	// ignore closing bracket
	_, err = dec.Token()
	if err != nil {
		log.Fatal(err)
	}
}

var strRegex string

func kvParse(str string) (string, string, error) {
	parts := regexp.MustCompile("=").Split(str, 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("expected KEY=VALUE got '%s'", str)
	}
	return parts[0], parts[1], nil
}

func deletePath(path string) {
    client := &http.Client{}

    // Create request
    req, err := http.NewRequest("DELETE", path, nil)
    if err != nil {
        log.Fatal(err)
    }

    // Fetch Request
    resp, err := client.Do(req)
    if err != nil {
        log.Fatal(err)
    }

    defer resp.Body.Close()
}

func (response *HTTPResponse) getResp(hostStr string) error {
	resp, err := http.Get(hostStr)
	response.GetResp = *resp
	return err
}
