package main

import (
	"Torrent/bencode"
	"bufio"
	"time"

	// "encoding/json"
	"fmt"
	"os"
)

type TrackerState struct {
	URL        string
	nextCheck  time.Time
	isQuerying bool
}

func main() {
	file, err := os.Open("test2.torrent")
	if err != nil {
		fmt.Println("Error opening file:", err)
		return
	}
	defer file.Close()

	reader := bufio.NewReader(file)

	data, err := bencode.Decode(reader)
	if err != nil {
		fmt.Println("Error decoding bencode data:", err)
		return
	}

	// prettyJSON, _ := json.MarshalIndent(data, "", "  ")
	// fmt.Println(string(prettyJSON))

	announce_list, ok := data["announce-list"].([]interface{})
	if !ok {
		panic("announce-list key not found or invalid")
	}

	announce_urls, err := extractUrls(announce_list)
	if err != nil {
		panic(fmt.Sprintf("error extracting announce URLs: %v", err))
	}

	var trackerData []TrackerState
	for _, url := range announce_urls {
		tracker := TrackerState{
			URL:        url,
			nextCheck:  time.Now(),
			isQuerying: false,
		}
		trackerData = append(trackerData, tracker)
	}

	info, ok := data["info"].(map[string]any)
	if !ok {
		panic(fmt.Errorf("info key not found"))
	}

	peerChan := make(chan []string)
	go RunTrackerManager(trackerData, info, peerChan)
	for peers := range peerChan {
		fmt.Printf("Received %d peers from tracker manager\n", len(peers))
		for _, peer := range peers {
			fmt.Println(peer)
		}
	}
}

func RunTrackerManager(states []TrackerState, info map[string]any, peerChan chan<- []string) {
	for {
		fmt.Println("--- Tracker Manager Tick ---")

		peers, err := ExtractPeers(states, info)
		if err != nil {
			fmt.Printf("Tracker manager encountered an error: %v\n", err)
		}

		if len(peers) > 0 {
			peerChan <- peers
		}

		time.Sleep(5 * time.Second)
	}
}

func extractUrls(announceList []interface{}) ([]string, error) {
	var urls []string
	for _, tier := range announceList {
		tierList, ok := tier.([]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid tier in announce list")
		}
		for _, url := range tierList {
			if urlStr, ok := url.(string); ok {
				urls = append(urls, urlStr)
			}
		}
	}
	return urls, nil
}
