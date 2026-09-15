## Go STAMP 
Package stamp implements the Simple Two-way Active Measurement Protocol
(STAMP) as defined in [RFC 8762](https://www.rfc-editor.org/rfc/rfc8762.pdf).
It provides two layers:
  - A low-level packet codec: Packet, Encode, Decode.
  - A high-level session API: Sender and Reflector.

Only the unauthenticated base mode is supported in v0. STAMP Optional
Extensions (RFC 8972) and HMAC authentication are out of scope for now.

## Segment Routing Extension (RFC 9503)
This library will also support SRv6 Segment Routing extensions for the STAMP protocol. Features will be limited to what Maeto actually needs ( Control Code 0x1 is not supported ).

Between link A-B, two stateless STAMP sessions are initiated. The probes are unidirectional and terminate at the reflector. 

The reasoning behind this is that having bidirectional STAMP sessions running between the two ends cost 2x probes. Bidirectional STAMP from only one end would require leader election and all the failover jargon that I don't want to deal with.

Running one way STAMP measurement with Reply Request = 0x0 from both sides gives us unidirectional measurements without having to deal with leader election or return path issues.

### Maeto-specific STAMP TLV: Maeto-Container Type 252
For unidirectional setup, the STAMP packet terminates at the far-end, which is now responsible for sending the collected telemetry upstream. Telemetry-Key Sub-TLV of Maeto-Container is the lookup key (or NATS subject) that the far-end will send the telemetry towards.

```
 0                  1                  2                  3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|STAMP TLV Flags|    Type=252   |             Length            |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
:                        Maeto Container                        :
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
```

#### Telemetry-Key Sub-TLV
The telemetry-key sub-TLV is identified by Type=0x1.
```
 0                  1                  2                  3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|STAMP TLV Flags|     Type=1    |             Length            |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
:                         Telemetry-Key                         :
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
```

### Use
**Note**: This document was written in **Apr-19-2026** and is subject to change in the future.

#### STAMP Config
```go
stampConfig := stamp.Config{
		ErrorEstimate: stamp.ErrorEstimateConfig{
			ClockFormat:  stamp.ClockFormatNTP,
			Multiplier:   1,
			Scale:        22,
			Synchronized: clockStateSynchronized,
		},
	}
```

#### Sender
```go
sender, err := stamp.NewSender(stamp.SenderConfig{
		LocalAddr:  p.SourceIP,
		RemoteAddr: net.JoinHostPort(target, targetPort),
		Timeout:    time.Duration(1) * time.Second,
		Config:     stampConfig,
		OnError:    func(err error) { logger.ErrorContext(ctx, "stamp sender error", "err", err) },
	})
	if err != nil {
        ...
	}
	defer sender.Close()

    reflectorPkt, err := sender.Send()
```

#### Reflector
```go
	reflector, err := stamp.NewReflector(stamp.ReflectorConfig{
		LocalAddr: net.JoinHostPort(sourceIP, listenPort),
		HMACKey:   nil,
		OnError: func(err error) {
			if ctx.Err() == nil {
				...
			}
		},
		Config: stampConfig,
	})

    err := reflector.Serve(ctx)
```
