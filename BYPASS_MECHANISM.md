# Censorship Bypass Mechanisms in Lumine

## Overview

Lumine is a censorship circumvention tool that uses various traffic manipulation techniques to bypass Deep Packet Inspection (DPI) systems. This document explains how these mechanisms work based on the actual implementation.

## How Censorship Works

Network censorship systems typically use Deep Packet Inspection (DPI) to:
1. **Identify TLS connections** by examining the ClientHello handshake
2. **Extract the Server Name Indication (SNI)** field to determine the destination
3. **Block or reset connections** to forbidden domains

## Bypass Techniques

### 1. TLS Record Fragmentation (tls-rf)

This is the primary bypass mechanism shown in the logs you provided.

#### How It Works

**Normal TLS Handshake:**
```
Client → [Complete ClientHello in one packet] → Server
         ↑ DPI can easily inspect SNI
```

**With TLS-RF:**
```
Client → [Fragment 1][Fragment 2][Fragment 3][Fragment 4] → Server
         ↑ SNI is split across multiple TLS records
```

#### Implementation Details

The `tls-rf` mode implements the following steps:

1. **Intercepts the ClientHello**: Captures the TLS ClientHello packet before it's sent
2. **Splits into Multiple Records**: Divides the ClientHello into multiple TLS records
3. **Strategic Fragmentation**: Specifically splits around the SNI field so the domain name is fragmented
4. **Optional Modifications**:
   - `mod_minor_ver`: Changes the TLS minor version in the record header (e.g., from 0x03 to 0x04)
   - `oob`: Attaches Out-Of-Band data to the first segment
   - `num_records`: Controls how many TLS records to create (e.g., 4 records in your logs)
   - `num_segs`: Controls TCP-level segmentation

#### Code Location
- Main logic: `fragment.go` - `sendRecords()` function
- Record splitting: `splitAndAppend()` function

#### Why This Works

DPI systems often have limitations:
- **Stateless Inspection**: Many DPI systems don't reassemble fragmented TLS records
- **Performance Constraints**: Reassembly requires buffering and processing power
- **Protocol Quirks**: The unusual minor version modification may confuse parsers

### 2. TTL-based Desynchronization (ttl-d)

This technique exploits the Time-To-Live (TTL) field in IP packets.

#### How It Works

**Step 1: TTL Probing**
```go
// minReachableTTL() in desync_linux.go
// Binary search to find minimum TTL that reaches the server
low, high := 1, maxTTL
for low <= high {
    mid := (low + high) / 2
    // Try to connect with TTL = mid
    // If successful, the server is reachable at this TTL
}
```

**Step 2: Send Fake Packet**
```
Client → [Fake ClientHello, TTL=5] → DPI System → [Dropped]
                                      ↓
                                   Server (unreachable, TTL expired)
```

The fake packet contains an invalid or truncated SNI that will be inspected by the DPI but won't reach the server.

**Step 3: Wait and Reset TTL**
```go
time.Sleep(fakeSleep) // Wait for DPI to process
```

**Step 4: Send Real Packet**
```
Client → [Real ClientHello, TTL=64] → DPI System → Server
                                      ↑ Already processed fake packet
                                      ↓ Ignores real packet
```

#### Implementation Details

On Linux (`desync_linux.go`):
- Uses `vmsplice()` and `splice()` for zero-copy packet manipulation
- Memory-mapped pages for efficient data handling
- Precise TTL control via `IP_TTL` socket option

On Windows (`desync_windows.go`):
- Uses `TransmitFile()` API for asynchronous packet transmission
- Temporary file-based approach for data staging
- `WSASend()` for network operations

#### Why This Works

1. **DPI State Confusion**: The DPI system processes the fake packet and may create a connection state
2. **Timing Window**: After processing the fake packet, there's a window where the real packet can pass through
3. **TTL Precision**: By setting the fake packet's TTL just below the threshold to reach the server, it's guaranteed to be dropped before reaching the destination

### 3. Minor Version Modification

When `mod_minor_ver` is enabled:

```go
// In fragment.go, sendRecords()
if modMinorVer {
    header[2] = 0x04  // Change minor version
}
```

Standard TLS 1.2 record header: `0x16 0x03 0x03`
Modified header: `0x16 0x03 0x04`

This subtle change:
- Is technically valid (servers typically ignore minor version in records)
- May confuse DPI pattern matching that expects exact byte sequences
- Maintains protocol compatibility

### 4. Out-Of-Band (OOB) Data

Sends OOB data (TCP urgent data) with the first segment:

```go
// Linux: desync_linux.go
unix.Sendto(int(fd), []byte{'&'}, unix.MSG_OOB, nil)

// Windows: desync_windows.go  
windows.WSASend(sock, &wsabuf, 1, &bytesSent, windows.MSG_OOB, nil, nil)
```

This exploits:
- Some DPI systems don't properly handle TCP urgent pointer
- OOB data can confuse packet inspection logic
- Creates ambiguity in packet boundaries

## Log Analysis

Based on your logs:

```
[H00002] 2026/02/09 06:09:52 Policy: 142.251.119.90 | tls-rf | 4 records | mod_minor_ver
```

This shows:
- **Target IP**: 142.251.119.90 (Google/YouTube)
- **Mode**: tls-rf (TLS Record Fragmentation)
- **Configuration**: 4 records with minor version modification
- **Result**: "Successfully sent ClientHello" - bypass succeeded

```
[S00256] 2026/02/09 06:09:52 Redirect 172.64.229.211 to www.ietf.org
```

This shows IP redirection:
- The config maps certain IPs to different hosts
- Used to bypass IP-based blocking
- Maintains DNS resolution flexibility

## Configuration Examples

### TLS-RF Mode (From your logs)
```json
{
  "domain_policy": {
    "*.youtube.com": {
      "mode": "tls-rf",
      "num_records": 4,
      "mod_minor_ver": true
    }
  }
}
```

### TTL-D Mode with Auto-Detection
```json
{
  "domain_policy": {
    "*.blocked-site.com": {
      "mode": "ttl-d",
      "fake_ttl": 0,
      "fake_sleep": "200ms",
      "attempts": 3,
      "max_ttl": 30
    }
  }
}
```

## Defense Against DPI

The techniques work because they exploit fundamental limitations in DPI systems:

1. **Computational Limits**: Full reassembly and state tracking is expensive
2. **Timing Constraints**: DPI must make fast decisions to avoid latency
3. **Protocol Complexity**: TLS and TCP have many edge cases
4. **Backwards Compatibility**: Servers must accept slightly malformed but valid data

## Limitations

These techniques may not work against:
- **Sophisticated DPI**: Systems that do full reassembly
- **Application-layer Proxies**: Man-in-the-middle TLS inspection
- **IP-based Blocking**: If the destination IP itself is blocked
- **Protocol Fingerprinting**: Detection of the manipulation patterns themselves

## References

- Code: `fragment.go`, `desync_linux.go`, `desync_windows.go`, `utils.go`
- Based on: [TlsFragment](https://github.com/maoist2009/TlsFragment)
- Related: Geneva (Genetic Evasion), GoodbyeDPI, PowerTunnel
