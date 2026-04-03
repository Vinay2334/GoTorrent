package main

import (
	"Torrent/bencode"
	"bufio"

	// "encoding/json"
	"fmt"
	"os"
)

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

	_, err = ExtractPeers(data)
	if err != nil {
		fmt.Println("Error extracting peers:", err)
	}
}
