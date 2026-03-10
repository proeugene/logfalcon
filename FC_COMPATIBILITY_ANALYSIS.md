# LogFalcon — Flight Controller Compatibility Analysis

> **Author**: Principal Software Engineer / Chief Business Analyst  
> **Date**: March 2026  
> **Scope**: Feasibility of supporting iNav and ArduPilot FCs alongside Betaflight

---

## Executive Summary

LogFalcon now supports **Betaflight and iNav**. This document analyses the feasibility work done to add iNav support (shipped in v0.2.0) and why ArduPilot support was deferred.

| FC Firmware | Effort | Feasibility | Estimated LOC Change | Business Value | Status |
|---|---|---|---|---|---|
| **iNav** | 🟢 Low–Medium | Excellent | ~150–200 lines | High — large user overlap | ✅ **SHIPPED in v0.2.0** |
| **ArduPilot** | 🔴 Very High | Questionable | ~1,500+ lines (new module) | Low — different user base, different workflow | ⛔ Deferred indefinitely |

**Result**: iNav support shipped in v0.2.0 with ~200 LOC across 9 files. Zero breaking changes. ArduPilot deferred — requires MAVLink protocol, better served by a separate tool.

---

## Table of Contents

1. [Current Architecture & Betaflight Coupling](#1-current-architecture--betaflight-coupling)
2. [iNav Compatibility Deep-Dive](#2-inav-compatibility-deep-dive)
3. [ArduPilot Compatibility Deep-Dive](#3-ardupilot-compatibility-deep-dive)
4. [Implementation Plan: iNav Support](#4-implementation-plan-inav-support)
5. [Implementation Plan: ArduPilot Support](#5-implementation-plan-ardupilot-support)
6. [Risk Assessment](#6-risk-assessment)
7. [Business Analysis](#7-business-analysis)
8. [Recommendation](#8-recommendation)

---

## 1. Current Architecture & Betaflight Coupling

### 1.1 MSP Commands Used by LogFalcon

| Code | Command | Origin | Purpose | Betaflight-Only? |
|------|---------|--------|---------|-----------------|
| 1 | `MSP_API_VERSION` | MultiWii base | Handshake | ❌ Generic |
| 2 | `MSP_FC_VARIANT` | MultiWii base | Identify FC type | ❌ Generic |
| 160 | `MSP_UID` | MultiWii base | Unique FC ID | ❌ Generic |
| 80 | `MSP_BLACKBOX_CONFIG` | Betaflight ext. | Query blackbox device | ⚠️ Betaflight ext. |
| 70 | `MSP_DATAFLASH_SUMMARY` | Betaflight ext. | Flash usage stats | ⚠️ Betaflight ext. |
| 71 | `MSP_DATAFLASH_READ` | Betaflight ext. | Stream flash data | ⚠️ Betaflight ext. |
| 72 | `MSP_DATAFLASH_ERASE` | Betaflight ext. | Erase flash | ⚠️ Betaflight ext. |

4 of 7 active commands are Betaflight extensions — and those 4 are the **core functionality**.

### 1.2 Coupling Points (5 locations)

| File | Line | What's Hardcoded |
|------|------|-----------------|
| `logfalcon/msp/constants.py:40` | `BTFL_VARIANT = b'BTFL'` | Variant constant |
| `logfalcon/fc/detector.py:67–68` | `if variant[:4] != BTFL_VARIANT: raise FCNotBetaflight` | **Hard reject** of any non-BTFL FC |
| `logfalcon/fc/detector.py:79` | `client.get_blackbox_config()` → MSP cmd 80 | Assumes Betaflight response format |
| `logfalcon/msp/client.py:176–192` | `_parse_flash_read_payload()` expects `addr(4B) + len(2B) + compression(1B) + data` | Betaflight-specific response layout |
| `logfalcon/storage/manifest.py:40` | `fc_dir = storage_root / f'fc_BTFL_uid-{uid_short}'` | Hardcoded `BTFL` in directory name |

### 1.3 MSP Framing

The framing layer (`logfalcon/msp/framing.py`) supports both MSPv1 and MSPv2 and is **fully generic** — no Betaflight-specific modifications. This is the one clean abstraction boundary.

---

## 2. iNav Compatibility Deep-Dive

### 2.1 Protocol Compatibility Matrix

| Feature | Betaflight | iNav | Match? |
|---------|-----------|------|--------|
| MSP framing (v1/v2) | ✅ | ✅ | ✅ Identical |
| `MSP_API_VERSION` (1) | ✅ | ✅ | ✅ Identical |
| `MSP_FC_VARIANT` (2) | `b'BTFL'` | `b'INAV'` | ⚠️ Different value |
| `MSP_UID` (160) | ✅ | ✅ | ✅ Identical |
| `MSP_DATAFLASH_SUMMARY` (70) | ✅ | ✅ | ✅ Same cmd code |
| `MSP_DATAFLASH_READ` (71) | ✅ | ✅ | ⚠️ **Different response format** |
| `MSP_DATAFLASH_ERASE` (72) | ✅ | ✅ | ✅ Identical |
| `MSP_BLACKBOX_CONFIG` (80) | ✅ Full response | ⚠️ Returns zeros | ⚠️ Deprecated in iNav |
| SPI flash support | ✅ | ✅ | ✅ Both support it |
| Huffman compression | ✅ Optional | ❌ Not supported | ⚠️ Must disable for iNav |

### 2.2 Critical Difference: DATAFLASH_READ Response Format

This is the **single most important technical difference**.

**Betaflight response** (what LogFalcon currently expects):
```
Byte 0-3:  Address echo      (uint32 LE)
Byte 4-5:  Data length        (uint16 LE)   ← iNav DOES NOT HAVE THIS
Byte 6:    Compression type   (uint8)        ← iNav DOES NOT HAVE THIS
Byte 7+:   Data payload
```

**iNav response**:
```
Byte 0-3:  Address echo      (uint32 LE)
Byte 4+:   Raw data payload                  ← Data starts immediately
```

iNav omits the 3-byte metadata header (data_size + compression_type). LogFalcon's `_parse_flash_read_payload()` would fail with `struct.unpack_from('<IHB', payload)` reading garbage.

### 2.3 BLACKBOX_CONFIG Difference

| | Betaflight (cmd 80) | iNav (cmd 80) | iNav (MSP2 0x201A) |
|---|---|---|---|
| Supported | ✅ Full | ⚠️ Returns all zeros | ✅ Full |
| Device type | ✅ Byte 1 | ❌ Zero | ✅ Byte 1 |
| Rate info | ✅ | ❌ | ✅ Extended |

LogFalcon uses MSP_BLACKBOX_CONFIG (80) to detect if the FC uses SD card vs SPI flash. **For iNav, this returns zeros** — the code would see `device=0` (NONE), which would incorrectly skip the SD card check but also not confirm flash support.

### 2.4 What Would Break If iNav FC Is Connected Today

```
Step 1: Auto-detect /dev/ttyACM* → ✅ Works
Step 2: MSP_API_VERSION → ✅ Returns version
Step 3: MSP_FC_VARIANT → Returns b'INAV'
Step 4: detector.py:67 → raise FCNotBetaflight ← ❌ HARD STOP
        LED = ERROR, Web UI = "Expected BTFL variant, got b'INAV'"
```

The user would see: **"Expected BTFL variant, got b'INAV'"** and the sync would abort.

### 2.5 iNav Market Data

- iNav is the **#2 flight controller firmware** after Betaflight
- Very popular for long-range and GPS-enabled quads/planes
- Large overlap with FPV community (Betaflight users often also fly iNav)
- Uses SPI flash for blackbox just like Betaflight
- Same field pain point — need to clear flash between flights

---

## 3. ArduPilot Compatibility Deep-Dive

### 3.1 Protocol Compatibility Matrix

| Feature | Betaflight | ArduPilot | Match? |
|---------|-----------|-----------|--------|
| MSP framing | ✅ | ✅ (limited) | ⚠️ MSP for OSD only |
| `MSP_DATAFLASH_SUMMARY` (70) | ✅ | ❌ **Not implemented** | ❌ |
| `MSP_DATAFLASH_READ` (71) | ✅ | ❌ **Not implemented** | ❌ |
| `MSP_DATAFLASH_ERASE` (72) | ✅ | ❌ **Not implemented** | ❌ |
| `MSP_BLACKBOX_CONFIG` (80) | ✅ | ❌ **Not implemented** | ❌ |
| Log download protocol | MSP | **MAVLink** | ❌ Fundamentally different |
| Log format | Betaflight BBL | ArduPilot DataFlash/BIN | ❌ Different format |
| SPI flash support | ✅ | ✅ (W25NXX, JEDEC) | ✅ Hardware exists |

### 3.2 ArduPilot's MSP Implementation

ArduPilot has MSP support (`libraries/AP_MSP/`) but **exclusively for telemetry/OSD** (DJI compatibility). The MSP handler has a `default: return MSP_RESULT_ACK` case that **silently acknowledges** unimplemented commands including all dataflash commands. This means:

- Sending `MSP_DATAFLASH_SUMMARY` would return an empty ACK (no data)
- LogFalcon would get a 0-byte payload and fail

### 3.3 ArduPilot Log Architecture

ArduPilot uses a **completely different logging stack**:

```
AP_Logger (libraries/AP_Logger/)
├── AP_Logger_File         → SD card via filesystem (default)
├── AP_Logger_W25NXX       → SPI W25N flash (Winbond)
├── AP_Logger_Flash_JEDEC  → Generic JEDEC SPI flash
├── AP_Logger_MAVLink      → Real-time MAVLink streaming
└── AP_Logger_Block        → Generic block storage (parent)
```

**Log download protocol**: MAVLink standard messages
```
LOG_REQUEST_LIST  → Get list of available logs
LOG_REQUEST_DATA  → Stream log data (chunked)
LOG_REQUEST_END   → Stop streaming
LOG_ERASE         → Erase all logs
```

**Constraint**: ArduPilot requires the vehicle to be **disarmed** before allowing log downloads.

### 3.4 What Would It Take

Supporting ArduPilot is essentially building a **second product**:

| Component | What's Needed |
|-----------|--------------|
| Protocol | Full MAVLink v2 client (framing, CRC, message parsing) |
| Discovery | MAVLink HEARTBEAT + AUTOPILOT_VERSION |
| Log listing | MAVLink LOG_REQUEST_LIST handler |
| Log download | MAVLink LOG_REQUEST_DATA chunked streaming |
| Log erase | MAVLink LOG_ERASE command |
| Format | ArduPilot .BIN format (different from BBL) |
| Dependencies | `pymavlink` library (~5MB) |
| Testing | Entirely new test suite for MAVLink path |

**Estimated**: 1,500+ new lines of code, new dependency, separate test matrix.

---

## 4. Implementation Plan: iNav Support

### 4.1 Approach: FC-Variant-Aware Protocol Handler

The cleanest approach is to make the MSP client aware of the FC variant and adjust response parsing accordingly. This avoids a full plugin/driver architecture (overkill at this stage).

### 4.2 Changes Required

#### Change 1: Accept iNav variant in detector (`detector.py`)
**Effort**: Trivial  
**Risk**: None

```python
# Before:
if variant[:4] != BTFL_VARIANT:
    raise FCNotBetaflight(...)

# After:
SUPPORTED_VARIANTS = {b'BTFL', b'INAV'}
if variant[:4] not in SUPPORTED_VARIANTS:
    raise FCNotSupported(f'Unsupported FC variant: {variant!r}')
```

Also rename `FCNotBetaflight` → `FCNotSupported` (or keep as alias for backwards compat).

#### Change 2: Handle iNav BLACKBOX_CONFIG (`detector.py`)
**Effort**: Small  
**Risk**: Low

iNav returns all-zeros for MSP_BLACKBOX_CONFIG (80). Options:
- **Option A**: Try MSP2_BLACKBOX_CONFIG (0x201A) as fallback
- **Option B**: Skip blackbox device check for iNav, go straight to DATAFLASH_SUMMARY (if it returns data, flash exists)
- **Recommended**: Option B (simpler, fewer protocol additions)

#### Change 3: Variant-aware DATAFLASH_READ parsing (`client.py`)
**Effort**: Medium — **this is the critical change**  
**Risk**: Medium

```python
def _parse_flash_read_payload(self, payload: bytes, variant: bytes = b'BTFL') -> tuple[int, bytes]:
    if variant == b'INAV':
        # iNav format: addr(4B) + raw_data
        if len(payload) < 4:
            raise MSPError(f'Short DATAFLASH_READ response (len={len(payload)})')
        chunk_addr = struct.unpack_from('<I', payload)[0]
        data = payload[4:]
    else:
        # Betaflight format: addr(4B) + len(2B) + compression(1B) + data
        if len(payload) < 7:
            raise MSPError(f'Short DATAFLASH_READ response (len={len(payload)})')
        chunk_addr, data_size, compression_type = struct.unpack_from('<IHB', payload)
        raw_data = payload[7 : 7 + data_size]
        if compression_type == DATAFLASH_COMPRESSION_HUFFMAN:
            char_count = struct.unpack_from('<H', raw_data)[0]
            data = huffman_decode(raw_data[2:], char_count)
        else:
            data = raw_data
    return chunk_addr, data
```

The variant needs to flow from detection → client. Options:
- Store variant on the MSPClient instance after detection
- Pass variant to each parse call from the orchestrator

#### Change 4: Disable compression for iNav (`client.py`)
**Effort**: Trivial  
**Risk**: None

```python
def send_flash_read_request(self, address: int, size: int, compression: bool = False) -> None:
    if self.fc_variant == b'INAV':
        compression = False  # iNav doesn't support Huffman
    payload = struct.pack('<IHB', address, size, 1 if compression else 0)
```

#### Change 5: Dynamic directory naming (`manifest.py`)
**Effort**: Trivial  
**Risk**: None

```python
# Before:
fc_dir = storage_root / f'fc_BTFL_uid-{uid_short}'

# After:
variant_str = fc_info.variant[:4].decode('ascii', errors='replace')
fc_dir = storage_root / f'fc_{variant_str}_uid-{uid_short}'
```

#### Change 6: Update constants (`constants.py`)
**Effort**: Trivial  
**Risk**: None

```python
INAV_VARIANT = b'INAV'
SUPPORTED_VARIANTS = {b'BTFL', b'INAV'}
```

#### Change 7: Update UX strings
**Effort**: Small  
**Risk**: None

- LED status messages, web UI text: "Betaflight FC" → "FC" or "Betaflight/iNav FC"
- README, docs: mention iNav compatibility

### 4.3 Files Modified

| File | Changes |
|------|---------|
| `logfalcon/msp/constants.py` | Add `INAV_VARIANT`, `SUPPORTED_VARIANTS` |
| `logfalcon/msp/client.py` | Variant-aware `_parse_flash_read_payload()`, `fc_variant` property |
| `logfalcon/fc/detector.py` | Accept iNav, rename exception, handle blackbox config fallback |
| `logfalcon/storage/manifest.py` | Dynamic `fc_{variant}_uid-*` directory naming |
| `logfalcon/sync/orchestrator.py` | Pass variant to client, disable compression for iNav |
| `logfalcon/web/_templates.py` | Update display strings |
| `tests/test_fc_detector.py` | Test iNav detection path |
| `tests/test_msp_client.py` | Test iNav response parsing |
| `README.md` | Update supported FC list |

### 4.4 Testing Strategy

- **Unit tests**: Mock iNav-format DATAFLASH_READ responses (no length/compression header)
- **Integration test**: If possible, use iNav SITL (Software-In-The-Loop) simulator
- **Hardware test**: Flash an iNav board and connect to Pi Zero W
- **Regression**: All existing Betaflight tests must still pass

### 4.5 Estimated Effort

| Category | Lines | Time |
|----------|-------|------|
| Core protocol changes | ~80 | — |
| Detection & manifest | ~30 | — |
| UX / docs | ~30 | — |
| Tests | ~60 | — |
| **Total** | **~200** | — |

---

## 5. Implementation Plan: ArduPilot Support

### 5.1 Architecture

ArduPilot would require a **parallel protocol path**, not a variant of the existing MSP flow:

```
                        ┌─────────────┐
USB Serial ────────────►│ Auto-Detect  │
                        │ (MSP or MAV) │
                        └──────┬───────┘
                       ┌───────┴───────┐
                       ▼               ▼
              ┌─────────────┐  ┌──────────────┐
              │ MSP Client  │  │ MAVLink Client│
              │ (BF / iNav) │  │ (ArduPilot)   │
              └──────┬──────┘  └──────┬────────┘
                     │                │
              ┌──────▼──────┐  ┌──────▼────────┐
              │ Flash Read  │  │ Log Download   │
              │ (MSP 70-72) │  │ (MAV LOG_*)    │
              └──────┬──────┘  └──────┬────────┘
                     │                │
                     └───────┬────────┘
                             ▼
                    ┌────────────────┐
                    │ Storage / Verify│
                    │ (SHA-256)       │
                    └────────────────┘
```

### 5.2 New Components Required

| Component | Description | Lines |
|-----------|-------------|-------|
| `logfalcon/mavlink/client.py` | MAVLink v2 framing, CRC, heartbeat | ~400 |
| `logfalcon/mavlink/log_transfer.py` | LOG_REQUEST_LIST/DATA/END/ERASE | ~300 |
| `logfalcon/mavlink/constants.py` | MAVLink message IDs, component IDs | ~50 |
| `logfalcon/fc/auto_detect.py` | Protocol auto-detection (MSP vs MAVLink) | ~100 |
| `logfalcon/sync/ardupilot_orchestrator.py` | ArduPilot-specific sync flow | ~300 |
| Tests | New test suite for MAVLink path | ~400 |
| **Total** | | **~1,550** |

### 5.3 Dependencies

- **`pymavlink`**: Python MAVLink library — adds ~5MB, well-maintained
- Alternative: Hand-roll minimal MAVLink v2 parser (~300 lines for just LOG_* messages)

### 5.4 Key Challenges

1. **Protocol auto-detection**: Need to probe serial port to determine if FC speaks MSP or MAVLink
2. **MAVLink handshake**: HEARTBEAT exchange, component ID negotiation
3. **Log enumeration**: ArduPilot has multiple logs (one per flight), not a single flash dump
4. **Disarm requirement**: ArduPilot won't allow log download while armed
5. **Image size**: `pymavlink` adds significant weight to the Pi image
6. **Testing**: No ArduPilot SITL on Pi Zero W (ARM), would need x86 CI

### 5.5 Verdict

**Not recommended for LogFalcon**. The effort is disproportionate to the value, and the user workflows are fundamentally different (ArduPilot users typically use Mission Planner/QGroundControl on laptops, which defeats LogFalcon's field-simplicity value proposition).

---

## 6. Risk Assessment

### iNav Support

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| DATAFLASH_READ format mismatch breaks parsing | High (confirmed) | High | FC-variant-aware parser with unit tests |
| BLACKBOX_CONFIG returns zeros | High (confirmed) | Medium | Fall through to DATAFLASH_SUMMARY |
| iNav firmware versions have different MSP behaviour | Low | Medium | Test against iNav 7.x, 8.x |
| Existing Betaflight users impacted by refactor | Low | High | Full regression test suite |
| iNav SPI flash layout differs from Betaflight | Very Low | Low | Raw dump — format doesn't matter to LogFalcon |

### ArduPilot Support

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| MAVLink implementation complexity | High | High | Use pymavlink |
| Protocol auto-detection false positives | Medium | High | Timeout-based fallback |
| Pi Zero W performance for MAVLink parsing | Low | Medium | Benchmark |
| Scope creep — "if ArduPilot, why not PX4/EmuFlight?" | High | Medium | Define clear FC support policy |

---

## 7. Business Analysis

### 7.1 Market Sizing (FPV / Drone FC Ecosystem)

| Firmware | Est. Market Share | SPI Flash Users | LogFalcon Relevance |
|----------|------------------|-----------------|-------------------|
| **Betaflight** | ~65% | High (most FC boards) | ✅ Supported today |
| **iNav** | ~20% | Medium-High (many shared boards) | 🟡 High value target |
| **ArduPilot** | ~10% | Low (mostly SD card) | 🔴 Low fit |
| **EmuFlight/Others** | ~5% | Low | ⚪ Not worth targeting |

### 7.2 iNav Value Proposition

- **Same pain point**: iNav pilots fly SPI-flash-equipped quads with identical blackbox workflow
- **Same hardware**: Many FC boards run both Betaflight and iNav (same SPI flash chip)
- **Field scenario identical**: Need to clear flash between flights without laptop
- **Community signal**: "Does this work with iNav?" is the #1 expected question at launch
- **Competitive moat**: No competing field-sync tool supports iNav either

### 7.3 ArduPilot Value Proposition (Weak)

- **Different workflow**: ArduPilot users predominantly use SD cards, not SPI flash
- **Laptop dependency**: ArduPilot users already carry laptops (Mission Planner / QGC required for pre-flight)
- **Log management**: ArduPilot logs are numbered files, not a flash dump — users already manage them via MAVLink
- **Different community**: Minimal overlap with FPV racing / freestyle community

### 7.4 ROI Comparison

| | iNav | ArduPilot |
|---|---|---|
| Development effort | ~200 LOC | ~1,500 LOC |
| New dependencies | None | pymavlink |
| Addressable market increase | +20–25% | +5–10% |
| Community goodwill | Very high | Moderate |
| Maintenance burden | Low (shared MSP code) | High (separate protocol) |
| **ROI** | **🟢 Excellent** | **🔴 Poor** |

---

## 8. Recommendation

### Phase 1: iNav Support (v0.2.0) — ✅ **SHIPPED**

iNav is now a first-class supported FC firmware as of v0.2.0. Changes made across 9 files (~200 LOC). No new dependencies. Full backwards compatibility with Betaflight workflows.

**Key changes shipped:**
- Variant-aware MSP parsing in `client.py` (iNav DATAFLASH_READ has no length/compression header)
- BLACKBOX_CONFIG skipped for iNav (deprecated command returns all-zeros)
- Dynamic `fc_{variant}_uid-*` directory naming in manifests
- Compression suppression for non-Betaflight FCs
- 7 new tests (166 total), all passing

### Phase 2: ArduPilot — **DO NOT DO THIS** (for now)

The protocol is fundamentally incompatible (MAVLink vs MSP), the user overlap is minimal, and the field-sync use case doesn't align with ArduPilot workflows. If demand materialises, consider it as a **separate product** (`LogFalcon-AP`) rather than shoehorning it into the MSP-based architecture.

### Phase 3: Future Considerations

- **EmuFlight**: Fork of Betaflight, uses identical MSP — likely works with zero changes once BTFL variant check is relaxed
- **KISS FC**: Proprietary protocol — not feasible
- **PX4**: Uses MAVLink — same challenges as ArduPilot

---

*This analysis was produced from source-code-level review of the Betaflight, iNav, and ArduPilot firmware repositories, the iNav Configurator, and the LogFalcon codebase.*
