package main

import (
	"Torrent/bencode"
	"crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type TrackerResult struct {
	URL   string
	Peers []string
	Err   error
}

// Helper to escape binary data exactly how BitTorrent trackers expect
func TrackerEscape(b []byte) string {
	var s strings.Builder
	for _, byteVal := range b {
		if (byteVal >= 'a' && byteVal <= 'z') || (byteVal >= 'A' && byteVal <= 'Z') ||
			(byteVal >= '0' && byteVal <= '9') || byteVal == '-' || byteVal == '_' || byteVal == '.' || byteVal == '~' {
			s.WriteByte(byteVal)
		} else {
			fmt.Fprintf(&s, "%%%02x", byteVal)
		}
	}
	return s.String()
}

func fetchPeers(trackerData []byte) ([]string, error) {
	var peers []string
	peerData := trackerData[20:] // Skip the 20-byte header
	for i := 0; i+6 <= len(peerData); i += 6 {
		ip := net.IPv4(peerData[i], peerData[i+1], peerData[i+2], peerData[i+3])
		port := binary.BigEndian.Uint16(peerData[i+4 : i+6])
		peers = append(peers, fmt.Sprintf("%s:%d", ip.String(), port))
	}
	if len(peers) == 0 {
		return nil, fmt.Errorf("no peers found in tracker response")
	}
	return peers, nil
}

func ExtractPeers(data []TrackerState, info map[string]interface{}, peerChan chan<- []string) error {
	port := 6881
	numwant := 50

	info_bencoded, err := bencode.Encode(info)
	if err != nil {
		return fmt.Errorf("error encoding info dictionary: %v", err)
	}

	h := sha1.New()
	h.Write(info_bencoded)
	info_hash := h.Sum(nil)

	var left int
	if length, ok := info["length"].(int64); ok {
		left = int(length)
	} else if files, ok := info["files"].([]interface{}); ok {
		for _, file := range files {
			if fileDict, ok := file.(map[string]interface{}); ok {
				if length, ok := fileDict["length"].(int64); ok {
					left += int(length)
				}
			}
		}
	}

	peer_id := make([]byte, 20)
	copy(peer_id, []byte("-GO0001-"))
	rand.Read(peer_id[8:])

	fmt.Printf("Info hash: %x\n", info_hash)

	for i := range data {
		go func(t *TrackerState) {
			var trackerRes []byte
			var err error
			if t.isQuerying || time.Now().Before(t.nextCheck) {
				t.Err = fmt.Errorf("Tracker not ready for querying")
				return
			}
			fmt.Printf("Querying tracker: %s\n", t.URL)
			t.isQuerying = true
			defer func() { t.isQuerying = false }()
			if strings.HasPrefix(t.URL, "udp://") {
				trackerRes, err = requestUDPTracker(t.URL, info_hash, peer_id, port, left, numwant)
			} else if strings.HasPrefix(t.URL, "http") {
				trackerRes, err = requestHTTPTracker(t.URL, info_hash, peer_id, port, left, numwant)
			} else {
				t.Err = fmt.Errorf("Unsupported tracker protocol")
				return
			}

			if err != nil {
				t.nextCheck = time.Now().Add(5 * time.Second)
				t.Err = fmt.Errorf("Error querying tracker: %v", err)
				return
			}

			fmt.Printf("Successfully fetched %d bytes from tracker %s\n", len(trackerRes), t.URL)

			interval := max(binary.BigEndian.Uint32(trackerRes[8:12]), 60)

			t.nextCheck = time.Now().Add(time.Duration(interval) * time.Second)

			peers, err := fetchPeers(trackerRes)
			if err != nil {
				t.Err = fmt.Errorf("Error parsing tracker response: %v", err)
				return
			}
			peerChan <- peers
		}(&data[i])
	}
	return nil
}

// ---------------------------------------------------------
// HTTP HANDLER
// ---------------------------------------------------------
func requestHTTPTracker(announce_url string, info_hash, peer_id []byte, port, left, numwant int) ([]byte, error) {
	params := map[string]string{
		"port":       strconv.Itoa(port),
		"uploaded":   "0",
		"downloaded": "0",
		"left":       strconv.FormatInt(int64(left), 10),
		"compact":    "1",
		"event":      "started",
		"numwant":    strconv.Itoa(numwant),
	}

	var query_parts []string
	query_parts = append(query_parts, "info_hash="+TrackerEscape(info_hash))
	query_parts = append(query_parts, "peer_id="+TrackerEscape(peer_id))
	fmt.Printf("Query parts: %v\n", query_parts)

	for key, value := range params {
		query_parts = append(query_parts, fmt.Sprintf("%s=%s", key, url.QueryEscape(value)))
	}

	query_string := strings.Join(query_parts, "&")
	fmt.Printf("Full query string: %s\n", query_string)

	if strings.Contains(announce_url, "?") {
		announce_url += "&" + query_string
	} else {
		announce_url += "?" + query_string
	}

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest("GET", announce_url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "WebTorrent/0.102.4")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}

// ---------------------------------------------------------
// UDP HANDLER (BEP 15)
// ---------------------------------------------------------
func requestUDPTracker(announce_url string, info_hash, peer_id []byte, port, left, numwant int) ([]byte, error) {
	parsedURL, err := url.Parse(announce_url)
	if err != nil {
		return nil, err
	}

	// 1. Open UDP Socket
	// conn, err := net.DialTimeout("udp", parsedURL.Host, 5*time.Second)
	udpAddr, err := net.ResolveUDPAddr("udp", parsedURL.Host)
	if err != nil {
		return nil, err
	}

	conn, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		return nil, err
	}

	conn.SetDeadline(time.Now().Add(5 * time.Second))
	defer conn.Close()

	// Generate a random transaction ID
	transID := make([]byte, 4)
	rand.Read(transID)
	transIDUint := binary.BigEndian.Uint32(transID)

	// 2. CONNECTION REQUEST
	connReq := make([]byte, 16)
	binary.BigEndian.PutUint64(connReq[0:8], 0x41727101980) // Magic constant for connection
	binary.BigEndian.PutUint32(connReq[8:12], 0)            // Action 0: Connect
	binary.BigEndian.PutUint32(connReq[12:16], transIDUint)

	// 3. Write specifically to that address
	if _, err = conn.Write(connReq); err != nil {
		return nil, fmt.Errorf("error sending connection request: %v", err)
	}

	connResp := make([]byte, 16)
	_, err = conn.Read(connResp)
	if err != nil {
		return nil, fmt.Errorf("error during connection handshake: %v", err)
	}

	// Check if action and transaction ID match
	if binary.BigEndian.Uint32(connResp[0:4]) != 0 || binary.BigEndian.Uint32(connResp[4:8]) != transIDUint {
		return nil, fmt.Errorf("invalid connection response")
	}

	connID := binary.BigEndian.Uint64(connResp[8:16])
	fmt.Printf("Received connection ID: %x\n", connID)

	// 4. ANNOUNCE REQUEST
	annReq := make([]byte, 100)
	binary.BigEndian.PutUint64(annReq[0:8], connID)
	binary.BigEndian.PutUint32(annReq[8:12], 1) // Action 1: Announce
	binary.BigEndian.PutUint32(annReq[12:16], transIDUint)
	copy(annReq[16:36], info_hash)
	copy(annReq[36:56], peer_id)
	binary.BigEndian.PutUint64(annReq[56:64], 0)            // Downloaded
	binary.BigEndian.PutUint64(annReq[64:72], uint64(left)) // Left
	binary.BigEndian.PutUint64(annReq[72:80], 0)            // Uploaded
	binary.BigEndian.PutUint32(annReq[80:84], 2)            // Event 2: Started
	binary.BigEndian.PutUint32(annReq[84:88], 0)            // IP address (0 = default)
	binary.BigEndian.PutUint32(annReq[88:92], transIDUint)  // Key (reusing transID is fine)
	binary.BigEndian.PutUint32(annReq[92:96], ^uint32(0))   // Num want
	binary.BigEndian.PutUint16(annReq[96:98], uint16(port)) // Port

	if _, err = conn.Write(annReq); err != nil {
		return nil, fmt.Errorf("error sending announce request: %v", err)
	}

	// Read Announce Response (20 bytes header + 6 bytes per peer)
	annResp := make([]byte, 2048)
	n, err := conn.Read(annResp)
	if err != nil {
		return nil, fmt.Errorf("error reading announce response: %v", err)
	}
	if n < 20 || binary.BigEndian.Uint32(annResp[0:4]) != 1 || binary.BigEndian.Uint32(annResp[4:8]) != transIDUint {
		return nil, fmt.Errorf("invalid announce response")
	}
	return annResp[:n], nil
}
