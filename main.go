package main

import (
	"Torrent/bencode"
	"Torrent/utils"
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"time"

	// "encoding/json"
	"fmt"
	"os"
)

type TrackerState struct {
	URL        string
	nextCheck  time.Time
	isQuerying bool
	Err        error
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

	info_bencoded, err := bencode.Encode(info)
	if err != nil {
		panic(fmt.Errorf("error encoding info dictionary: %v", err))
	}

	var info_hash [20]byte
	h := sha1.New()
	h.Write(info_bencoded)
	copy(info_hash[:], h.Sum(nil))

	var peer_id [20]byte
	copy(peer_id[:], []byte("-GO0001-"))
	rand.Read(peer_id[8:])

	var left int64
	if length, ok := info["length"].(int64); ok {
		left = length
	} else if files, ok := info["files"].([]interface{}); ok {
		for _, file := range files {
			if fileDict, ok := file.(map[string]interface{}); ok {
				if length, ok := fileDict["length"].(int64); ok {
					left += length
				}
			}
		}
	}

	peerChan := make(chan []string)
	go RunTrackerManager(trackerData, left, info_hash, peer_id, peerChan)

	pm := utils.NewPieceManager(left, info["piece length"].(int64))
	for peers := range peerChan {
		fmt.Printf("Received %d peers from tracker manager\n", len(peers))
		for _, peerAddr := range peers {
			go func(addr string) {
				fmt.Printf("Attempting handshake with peer: %s\n", addr)
				err := StartPeerHandshake(addr, info_hash, peer_id, pm)
				if err != nil {
					fmt.Printf("Handshake failed with peer %s: %v\n", addr, err)
				}
			}(peerAddr)
		}
	}
}

func RunTrackerManager(states []TrackerState, left int64, info_hash [20]byte, peer_id [20]byte, peerChan chan<- []string) {
	for {
		fmt.Println("--- Tracker Manager Tick ---")

		err := ExtractPeers(states, left, info_hash, peer_id, peerChan)
		if err != nil {
			fmt.Printf("Tracker manager encountered an error: %v\n", err)
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
