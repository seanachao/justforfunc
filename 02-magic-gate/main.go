// Copyright 2016 Google Inc. All rights reserved.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to writing, software distributed
// under the License is distributed on a "AS IS" BASIS, WITHOUT WARRANTIES OR
// CONDITIONS OF ANY KIND, either express or implied.
//
// See the License for the specific language governing permissions and
// limitations under the License.

package magicgate

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"time"

	"golang.org/x/net/context"

	"google.golang.org/appengine"
	"google.golang.org/appengine/log"
	"google.golang.org/appengine/urlfetch"
)

func init() {
	http.HandleFunc("/", handler)
}

const (
	welcomeMsg = `<?xml version="1.0" encoding="UTF-8"?>
<Response>
    <Say>Hello Gopher, I took this demo down. Sorry!</Say>
</Response>`
	comeInMsg = `<?xml version="1.0" encoding="UTF-8"?>
<Response>
    <Say>Welcome home!</Say>
    <Play digits="9"></Play>
</Response>`
	goAwayMsg = `<?xml version="1.0" encoding="UTF-8"?>
<Response>
    <Say>Go away, you evil person</Say>
</Response>`
)

func handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/xml")

	rec := r.FormValue("RecordingUrl")
	if rec == "" {
		fmt.Fprint(w, welcomeMsg)
		return
	}
	c := appengine.NewContext(r)
	text, err := transcribe(c, rec)
	if err != nil {
		http.Error(w, "could not transcribe", http.StatusInternalServerError)
		log.Errorf(c, "could not transcribe: %v", err)
		return
	}

	log.Infof(c, "transcription: %s", text)
	if text == "hello gopher" {
		fmt.Fprint(w, comeInMsg)
	} else {
		fmt.Fprint(w, goAwayMsg)
	}
}

func transcribe(c context.Context, url string) (string, error) {
	b, err := fetchAudio(c, url)
	if err != nil {
		return "", err
	}

	return fetchTranscription(c, b)
}

func fetchAudio(c context.Context, url string) ([]byte, error) {
	client := urlfetch.Client(c)
	res, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("could not fetch %v: %v", url, err)
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch with status %s", res.Status)
	}
	defer res.Body.Close()
	b, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("could not read response: %v", err)
	}
	return b, nil
}

var speechURL = "https://speech.googleapis.com/v1beta1/speech:syncrecognize?key=" +
	os.Getenv("SPEECH_API_KEY")

type speechReq struct {
	Config struct {
		Encoding   string `json:"encoding"`
		SampleRate int    `json:"sampleRate"`
	} `json:"config"`
	Audio struct {
		Content string `json:"content"`
	} `json:"audio"`
}

func fetchTranscription(c context.Context, b []byte) (string, error) {
	var req speechReq
	req.Config.Encoding = "LINEAR16"
	req.Config.SampleRate = 8000
	req.Audio.Content = base64.StdEncoding.EncodeToString(b)

	j, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("could not encode speech request: %v", err)
	}
	client := urlfetch.Client(c)
	client.Timeout = 15 * time.Second
	res, err := client.Post(speechURL, "application/json", bytes.NewReader(j))
	if err != nil {
		return "", fmt.Errorf("could not transcribe: %v", err)
	}

	var data struct {
		Error struct {
			Code    int
			Message string
			Status  string
		}
		Results []struct {
			Alternatives []struct {
				Transcript string
				Confidence float64
			}
		}
	}
	defer res.Body.Close()
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return "", fmt.Errorf("could not decode speech response: %v", err)
	}
	if data.Error.Code != 0 {
		return "", fmt.Errorf("speech API error: %d %s %s",
			data.Error.Code, data.Error.Status, data.Error.Message)
	}
	if len(data.Results) == 0 || len(data.Results[0].Alternatives) == 0 {
		return "", fmt.Errorf("no transcriptions found")
	}
	return data.Results[0].Alternatives[0].Transcript, nil
}
