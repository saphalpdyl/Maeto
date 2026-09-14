//go:build integration

package integration_test

import (
	"encoding/json"
	"log"
	"os"
	"testing"
	"time"

	"github.com/saphalpdyl/maeto/libs/stamp"
	"github.com/stretchr/testify/assert"
)

var config stamp.Config

func suiteAddr() string {
	if addr := os.Getenv("STAMP_REFLECTOR_ADDR"); addr != "" {
		return addr
	}
	return "localhost:862"
}

func TestMain(m *testing.M) {
	config = stamp.Config{
		ErrorEstimate: stamp.ErrorEstimateConfig{
			Scale:        22,
			Multiplier:   1,
			Synchronized: true,
			ClockFormat:  stamp.ClockFormatNTP,
		},
	}

	m.Run()
}

func Test_SendNormalPkt(t *testing.T) {
	t.Run("send normal packet", func(t *testing.T) {
		senderConfig := stamp.SenderConfig{
			Config:     config,
			LocalAddr:  ":0",
			RemoteAddr: suiteAddr(),
			HMACKey:    nil,
			Timeout:    5 * time.Second,
			OnError: func(err error) {
				t.Logf("error %v", err)
			},
		}
		sender, err := stamp.NewSender(senderConfig)
		if err != nil {
			t.Fatalf("failed to create sender: %v", err)
		}

		response, err := sender.Send()
		if err != nil {
			t.Fatalf("failed to send packet: %v", err)
		}

		responseM, err := json.Marshal(response)
		if err != nil {
			t.Fatalf("failed to marshal response: %v", err)
		}

		t.Logf("got response: %s", string(responseM))

		// timestamp sequence
		// session-sender timestamp < recieve-timestamp < timestamp RFC 8762§4.3.1
		timestamp, err1 := response.Timestamp.ToTime(senderConfig.Config.ErrorEstimate.ClockFormat)
		recieveTimestamp, err2 := response.ReceiveTimestamp.ToTime(sender.Config.ErrorEstimate.ClockFormat)
		senderTimestamp, err3 := response.SenderTimestamp.ToTime(sender.Config.ErrorEstimate.ClockFormat)

		if err1 != nil || err2 != nil || err3 != nil {
			log.Fatalf("failed to parse NTP timestamps to time.Time: %v %v %v", err1, err2, err3)
		}

		assert.Greater(t, *timestamp, *recieveTimestamp)
		assert.Greater(t, *recieveTimestamp, *senderTimestamp)
	})
}
