package main

import (
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
)

const upstreamBaseURL = "https://devstreaming-cdn.apple.com/videos/streaming/examples/bipbop_4x3/gear1/prog_index.m3u8"

// Upgraded mock VAST XML with Impression tracking URLs
const mockVastXML = `
<VAST version="3.0">
  <Ad id="fegoco-ad-101">
    <InLine>
      <Impression><![CDATA[https://analytics.fegoco-monetize.internal/track/impression?id=101]]></Impression>
      <Creatives>
        <Creative>
          <Linear>
            <MediaFiles>
              <MediaFile delivery="progressive" type="video/mp4" width="1280" height="720">
                https://devstreaming-cdn.apple.com/videos/streaming/examples/bipbop_4x3/gear1/fileSequence0.ts
              </MediaFile>
            </MediaFiles>
          </Linear>
        </Creative>
      </Creatives>
    </InLine>
  </Ad>
</VAST>
`

type VAST struct {
	Ads []Ad `xml:"Ad"`
}

type Ad struct {
	InLine InLine `xml:"InLine"`
}

type InLine struct {
	Impression string     `xml:"Impression"`
	Creatives  []Creative `xml:"Creatives>Creative"`
}

type Creative struct {
	Linear Linear `xml:"Linear"`
}

type Linear struct {
	MediaFiles []MediaFile `xml:"MediaFiles>MediaFile"`
}

type MediaFile struct {
	URL string `xml:",chardata"`
}

// Parsed Ad structure to hold media and tracking data
type AdData struct {
	MediaURL      string
	ImpressionURL string
}

func parseVAST() AdData {
	var vast VAST
	_ = xml.Unmarshal([]byte(strings.TrimSpace(mockVastXML)), &vast)

	var data AdData
	if len(vast.Ads) > 0 {
		data.ImpressionURL = strings.TrimSpace(vast.Ads[0].InLine.Impression)
		creatives := vast.Ads[0].InLine.Creatives
		if len(creatives) > 0 {
			mediaFiles := creatives[0].Linear.MediaFiles
			if len(mediaFiles) > 0 {
				data.MediaURL = strings.TrimSpace(mediaFiles[0].URL)
			}
		}
	}
	return data
}

func fireImpression(trackingURL string) {
	if trackingURL == "" {
		return
	}
	// In production, you'd make an HTTP GET/POST to this tracking URL.
	// For now, we simulate the server-side telemetry ping securely.
	log.Printf("[FEGOCO Telemetry] 📊 Impression verified! Firing tracking ping to ADS: %s", trackingURL)
}

func proxyManifestHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("Incoming manifest request from: %s", r.RemoteAddr)

	resp, err := http.Get(upstreamBaseURL)
	if err != nil {
		http.Error(w, "Failed to fetch upstream content", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "Failed to read upstream manifest", http.StatusInternalServerError)
		return
	}

	// Parse VAST payload
	adData := parseVAST()

	manifestStr := string(bodyBytes)
	lines := strings.Split(manifestStr, "\n")
	var modifiedLines []string

	adInjected := false
	for _, line := range lines {
		if strings.HasSuffix(line, ".ts") && !adInjected && adData.MediaURL != "" {
			log.Printf("[FEGOCO SSAI] Stitched VAST Ad: %s", adData.MediaURL)

			modifiedLines = append(modifiedLines, "#EXTINF:5.000,")
			modifiedLines = append(modifiedLines, adData.MediaURL)

			// Fire tracking impression server-side
			go fireImpression(adData.ImpressionURL)

			adInjected = true
		}
		modifiedLines = append(modifiedLines, line)
	}

	finalManifest := strings.Join(modifiedLines, "\n")

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Range")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(finalManifest))
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/stream/manifest.m3u8", proxyManifestHandler)

	port := ":8080"
	fmt.Printf("FEGOCO Monetize Telemetry Proxy running on http://localhost%s...\n", port)
	if err := http.ListenAndServe(port, mux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
