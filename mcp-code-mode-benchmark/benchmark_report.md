# MCP Code Mode Benchmark Report

- **Model:** anthropic/claude-sonnet-4-6
- **Binding Level:** Tool-level
- **Token Measurement:** Cumulative across all agent loop turns

---

## Summary


| Metric        | R1 OFF | R1 ON | R1 Change | R2 OFF | R2 ON  | R2 Change | R3 OFF | R3 ON  | R3 Change  |
| ------------- | ------ | ----- | --------- | ------ | ------ | --------- | ------ | ------ | ---------- |
| Servers/Tools | 6/96   | 6/96  |           | 11/251 | 11/251 |           | 16/508 | 16/508 |            |
| Queries       | 64     | 64    |           | 65     | 65     |           | 65     | 65     |            |
| Pass Rate     | 100%   | 100%  | —         | 98.5%  | 100%   | +1 query  | 100%   | 100%   | —          |
| Input Tokens  | 19.9M  | 8.3M  | -58.2%    | 35.7M  | 5.5M   | -84.5%    | 75.1M  | 5.4M   | **-92.8%** |
| Total Tokens  | 20.1M  | 8.5M  | -57.7%    | 35.8M  | 5.6M   | -84.3%    | 75.1M  | 5.5M   | **-92.7%** |
| Est. Cost     | $104   | $46   | -55.7%    | $180   | $30    | -83.4%    | $377   | $29    | **-92.2%** |
| Latency       | 85.8m  | 62.8m | -26.8%    | 37.6m  | 35.1m  | -6.8%     | 43.8m  | 36.3m  | -17.1%     |


> **R1** = Round 1 (96 tools, 6 servers) · **R2** = Round 2 (251 tools, 11 servers) · **R3** = Round 3 (508 tools, 16 servers)
> **OFF** = Code Mode OFF (raw tools) · **ON** = Code Mode ON (tool-level binding)

---

# Round 1 — 96 tools, 6 servers

**Eval Set:** 64 queries across 6 MCP servers (96 tools)

---

## Overall Summary


| Metric                 | Code Mode OFF | Code Mode ON | Change     |
| ---------------------- | ------------- | ------------ | ---------- |
| Pass Rate              | 64/64 (100%)  | 64/64 (100%) | —          |
| Total Input Tokens     | 19,867,724    | 8,306,687    | **-58.2%** |
| Total Output Tokens    | 188,189       | 181,175      | -3.7%      |
| Total Tokens           | 20,055,913    | 8,487,862    | **-57.7%** |
| Total Wall Clock       | 85.8 min      | 62.8 min     | **-26.8%** |
| Est. Cost (Sonnet 4.6) | $104.04       | $46.06       | **-55.7%** |


---

## By Difficulty


| Difficulty  | Queries | OFF Avg In | ON Avg In  | In Reduction | OFF Avg Lat | ON Avg Lat | Speedup |
| ----------- | ------- | ---------- | ---------- | ------------ | ----------- | ---------- | ------- |
| simple      | 20      | 122.5k     | 26.5k      | 78%          | 20s         | 21s        | -7%     |
| medium      | 22      | 247.0k     | 68.2k      | 72%          | 68s         | 50s        | 26%     |
| hard        | 14      | 628.4k     | 381.6k     | 39%          | 163s        | 122s       | 23%     |
| edge        | 8       | 414.8k     | 116.8k     | 72%          | 127s        | 66s        | 48%     |
| **Overall** | **64**  | **310.4k** | **129.8k** | **58%**      | **80s**     | **59s**    | **27%** |


---

## Per-Query Breakdown


| #   | ID        | Diff   | OFF In       | OFF Out    | OFF Total    | ON In       | ON Out     | ON Total    | In Reduction |
| --- | --------- | ------ | ------------ | ---------- | ------------ | ----------- | ---------- | ----------- | ------------ |
| 1   | S1        | simple | 117.6k       | 347        | 117.9k       | 15.6k       | 511        | 16.1k       | 87%          |
| 2   | S2        | simple | 119.3k       | 540        | 119.9k       | 22.9k       | 817        | 23.7k       | 81%          |
| 3   | S3        | simple | 117.6k       | 187        | 117.8k       | 19.0k       | 446        | 19.5k       | 84%          |
| 4   | S4        | simple | 157.2k       | 313        | 157.5k       | 15.5k       | 344        | 15.8k       | 90%          |
| 5   | S5        | simple | 117.6k       | 238        | 117.8k       | 18.8k       | 460        | 19.3k       | 84%          |
| 6   | S6        | simple | 118.2k       | 583        | 118.7k       | 21.1k       | 781        | 21.9k       | 82%          |
| 7   | S7        | simple | 118.1k       | 535        | 118.7k       | 20.1k       | 596        | 20.7k       | 83%          |
| 8   | S8        | simple | 78.3k        | 199        | 78.5k        | 17.1k       | 778        | 17.9k       | 78%          |
| 9   | S9        | simple | 120.2k       | 766        | 121.0k       | 16.1k       | 595        | 16.6k       | 87%          |
| 10  | S10       | simple | 39.1k        | 119        | 39.2k        | 16.9k       | 706        | 17.6k       | 57%          |
| 11  | S11       | simple | 80.6k        | 768        | 81.4k        | 25.2k       | 1.3k       | 26.4k       | 69%          |
| 12  | S12       | simple | 117.8k       | 453        | 118.2k       | 11.3k       | 515        | 11.8k       | 90%          |
| 13  | S13       | simple | 197.2k       | 778        | 198.0k       | 77.4k       | 4.4k       | 81.9k       | 61%          |
| 14  | S14       | simple | 157.6k       | 940        | 158.5k       | 69.2k       | 2.6k       | 71.8k       | 56%          |
| 15  | S18       | simple | 237.5k       | 775        | 238.3k       | 35.5k       | 561        | 36.0k       | 85%          |
| 16  | S19       | simple | 79.4k        | 728        | 80.1k        | 15.6k       | 878        | 16.5k       | 80%          |
| 17  | S20       | simple | 78.4k        | 266        | 78.7k        | 12.0k       | 540        | 12.6k       | 85%          |
| 18  | NT1       | simple | 159.7k       | 534        | 160.3k       | 51.9k       | 1.0k       | 52.9k       | 68%          |
| 19  | NT2       | simple | 78.6k        | 372        | 78.9k        | 12.8k       | 661        | 13.5k       | 84%          |
| 20  | NT3       | simple | 160.0k       | 876        | 160.9k       | 36.1k       | 1.1k       | 37.2k       | 77%          |
| 21  | M1        | medium | 157.6k       | 540        | 158.2k       | 20.3k       | 698        | 21.0k       | 87%          |
| 22  | M2        | medium | 158.9k       | 1.6k       | 160.5k       | 25.6k       | 2.2k       | 27.7k       | 84%          |
| 23  | M4        | medium | 171.7k       | 1.2k       | 172.9k       | 81.5k       | 1.5k       | 83.0k       | 53%          |
| 24  | M5        | medium | 277.5k       | 1.2k       | 278.7k       | 39.6k       | 1.9k       | 41.5k       | 86%          |
| 25  | M6        | medium | 118.2k       | 1.3k       | 119.5k       | 37.8k       | 1.5k       | 39.3k       | 68%          |
| 26  | M7        | medium | 218.5k       | 2.4k       | 220.9k       | 68.5k       | 2.6k       | 71.1k       | 69%          |
| 27  | M8        | medium | 279.2k       | 2.9k       | 282.2k       | 60.2k       | 6.5k       | 66.7k       | 78%          |
| 28  | M9        | medium | 198.2k       | 1.1k       | 199.3k       | 27.0k       | 977        | 28.0k       | 86%          |
| 29  | M10       | medium | 241.2k       | 1.7k       | 242.9k       | 68.0k       | 2.2k       | 70.3k       | 72%          |
| 30  | M11       | medium | 317.0k       | 1.0k       | 318.1k       | 84.7k       | 1.9k       | 86.7k       | 73%          |
| 31  | M13       | medium | 331.3k       | 2.2k       | 333.5k       | 44.2k       | 1.9k       | 46.0k       | 87%          |
| 32  | M14       | medium | 248.2k       | 2.6k       | 250.8k       | 28.3k       | 1.9k       | 30.2k       | 89%          |
| 33  | M15       | medium | 359.0k       | 1.5k       | 360.4k       | 42.7k       | 1.7k       | 44.4k       | 88%          |
| 34  | M16       | medium | 201.6k       | 2.1k       | 203.7k       | 138.5k      | 1.5k       | 140.0k      | 31%          |
| 35  | M17       | medium | 382.0k       | 5.1k       | 387.1k       | 67.0k       | 2.7k       | 69.7k       | 82%          |
| 36  | M18       | medium | 241.0k       | 4.7k       | 245.7k       | 57.5k       | 2.7k       | 60.3k       | 76%          |
| 37  | M19       | medium | 165.7k       | 1.6k       | 167.4k       | 51.1k       | 1.9k       | 53.0k       | 69%          |
| 38  | M20       | medium | 328.5k       | 4.1k       | 332.7k       | 114.4k      | 4.4k       | 118.8k      | 65%          |
| 39  | M22       | medium | 333.0k       | 2.8k       | 335.8k       | 115.6k      | 1.7k       | 117.3k      | 65%          |
| 40  | NT4       | medium | 336.3k       | 4.7k       | 341.0k       | 221.1k      | 3.7k       | 224.9k      | 34%          |
| 41  | NT5       | medium | 204.3k       | 1.5k       | 205.8k       | 63.9k       | 1.9k       | 65.8k       | 69%          |
| 42  | NT6       | medium | 164.0k       | 2.8k       | 166.8k       | 42.4k       | 3.0k       | 45.4k       | 74%          |
| 43  | H1        | hard   | 244.0k       | 2.4k       | 246.4k       | 283.3k      | 3.6k       | 286.9k      | -16%         |
| 44  | H2        | hard   | 731.0k       | 8.0k       | 739.0k       | 748.5k      | 7.2k       | 755.7k      | -2%          |
| 45  | H3        | hard   | 600.7k       | 4.4k       | 605.1k       | 483.5k      | 3.9k       | 487.4k      | 20%          |
| 46  | H5        | hard   | 612.3k       | 9.6k       | 622.0k       | 257.8k      | 8.1k       | 265.9k      | 58%          |
| 47  | H6        | hard   | 413.2k       | 4.2k       | 417.4k       | 50.7k       | 3.8k       | 54.5k       | 88%          |
| 48  | H7        | hard   | 1038.1k      | 8.2k       | 1046.3k      | 330.6k      | 10.6k      | 341.2k      | 68%          |
| 49  | H8        | hard   | 612.8k       | 5.5k       | 618.3k       | 826.7k      | 4.5k       | 831.2k      | -35%         |
| 50  | H10       | hard   | 415.5k       | 5.1k       | 420.7k       | 275.0k      | 8.6k       | 283.6k      | 34%          |
| 51  | H11       | hard   | 622.7k       | 6.3k       | 629.0k       | 231.2k      | 4.1k       | 235.3k      | 63%          |
| 52  | H12       | hard   | 1584.6k      | 10.6k      | 1595.2k      | 793.5k      | 9.2k       | 802.7k      | 50%          |
| 53  | H13       | hard   | 629.8k       | 16.5k      | 646.3k       | 468.4k      | 5.8k       | 474.3k      | 26%          |
| 54  | H15       | hard   | 496.7k       | 6.3k       | 503.1k       | 231.4k      | 6.8k       | 238.1k      | 53%          |
| 55  | NT7       | hard   | 296.9k       | 2.0k       | 298.9k       | 109.5k      | 2.5k       | 112.0k      | 63%          |
| 56  | NT8       | hard   | 368.0k       | 4.3k       | 372.3k       | 252.1k      | 5.4k       | 257.5k      | 31%          |
| 57  | E1        | edge   | 117.8k       | 939        | 118.8k       | 25.8k       | 1.6k       | 27.4k       | 78%          |
| 58  | E2        | edge   | 78.3k        | 226        | 78.6k        | 8.2k        | 336        | 8.5k        | 90%          |
| 59  | E3        | edge   | 247.8k       | 4.3k       | 252.1k       | 72.3k       | 6.1k       | 78.4k       | 71%          |
| 60  | E4        | edge   | 200.4k       | 1.5k       | 201.9k       | 58.9k       | 2.3k       | 61.2k       | 71%          |
| 61  | E5        | edge   | 236.9k       | 673        | 237.5k       | 29.8k       | 969        | 30.8k       | 87%          |
| 62  | E6        | edge   | 399.3k       | 8.7k       | 408.0k       | 81.8k       | 8.1k       | 89.9k       | 80%          |
| 63  | E8        | edge   | 1839.4k      | 16.8k      | 1856.1k      | 593.6k      | 5.6k       | 599.1k      | 68%          |
| 64  | NT9       | edge   | 198.1k       | 732        | 198.9k       | 63.9k       | 1.4k       | 65.3k       | 68%          |
|     | **TOTAL** |        | **19867.7k** | **188.2k** | **20055.9k** | **8306.7k** | **181.2k** | **8487.9k** | **58.2%**    |


---

# Round 2 — 251 tools, 11 servers

**Eval Set:** 65 queries across 11 MCP servers (251 tools)

## Code Mode OFF


| Metric                 | Value         |
| ---------------------- | ------------- |
| Pass Rate              | 64/65 (98.5%) |
| Total Input Tokens     | 35,692,851    |
| Total Output Tokens    | 64,090        |
| Total Tokens           | 35,756,941    |
| Total Wall Clock       | 37.6 min      |
| Avg Input Tokens/Query | ~549k         |
| Est. Cost (Sonnet 4.6) | $180.07       |


### By Difficulty


| Difficulty | Queries | Pass  | Avg Input | Avg Latency |
| ---------- | ------- | ----- | --------- | ----------- |
| Simple     | 27      | 27/27 | 316.3k    | 17.0s       |
| Medium     | 38      | 37/38 | 714.5k    | 47.3s       |


### Observations (Code Mode OFF)

- **R2M6 (FAIL)**: Model called `write_file` with `{"path": "apollo_usage.txt"}`, omitting the required `content` parameter, even though its clearly mentioned in the list of tools that content along with path is required. Retried 60 times with identical broken arguments until timeout. The MCP server returned a validation error each time but the model never self-corrected.
- **Non-existent tool hallucination**: In 6 queries (R2S7, R2M3, R2M13, R2M22, R2M25, R2H10), the model called `LINEAR_GET_ALL_LINEAR_TEAMS` — a tool that doesn't exist. The actual tool `LINEAR_LIST_LINEAR_TEAMS` has a description that references this non-existent tool. All 6 passed on re-run when the model chose the correct tool.
- **Token baseline**: With 251 tools, each API call sends ~135k tokens of tool schemas (vs ~39k with 96 tools in Round 1). Over a multi-turn agent loop, this compounds — a simple 2-turn query consumes ~270k input tokens, while medium queries with 5+ turns reach 500k–1M+.

## Code Mode ON


| Metric                 | Value        |
| ---------------------- | ------------ |
| Pass Rate              | 65/65 (100%) |
| Total Input Tokens     | 5,526,986    |
| Total Output Tokens    | 86,710       |
| Total Tokens           | 5,613,696    |
| Total Wall Clock       | 35.1 min     |
| Avg Input Tokens/Query | ~85k         |
| Est. Cost (Sonnet 4.6) | $29.80       |


### By Difficulty


| Difficulty | Queries | Pass  | Avg Input | Avg Latency |
| ---------- | ------- | ----- | --------- | ----------- |
| Simple     | 27      | 27/27 | 25.1k     | 16.9s       |
| Medium     | 38      | 38/38 | 127.6k    | 43.4s       |


## Round 2 Comparison


| Metric                 | Code Mode OFF | Code Mode ON | Change     |
| ---------------------- | ------------- | ------------ | ---------- |
| Pass Rate              | 64/65 (98.5%) | 65/65 (100%) | +1 query   |
| Total Input Tokens     | 35,692,851    | 5,526,986    | **-84.5%** |
| Total Output Tokens    | 64,090        | 86,710       | +35.3%     |
| Total Tokens           | 35,756,941    | 5,613,696    | **-84.3%** |
| Total Wall Clock       | 37.6 min      | 35.1 min     | -6.8%      |
| Est. Cost (Sonnet 4.6) | $180.07       | $29.80       | **-83.4%** |


### By Difficulty


| Difficulty  | Queries | OFF Avg In | ON Avg In | In Reduction | OFF Avg Lat | ON Avg Lat |
| ----------- | ------- | ---------- | --------- | ------------ | ----------- | ---------- |
| simple      | 27      | 316.3k     | 25.1k     | 92%          | 17.0s       | 16.9s      |
| medium      | 38      | 714.5k     | 127.6k    | 82%          | 47.3s       | 43.4s      |
| **Overall** | **65**  | **549.1k** | **85.0k** | **84.5%**    | **34.7s**   | **32.4s**  |


### Per-Query Comparison


| #   | ID        | Diff   | OFF/ON    | OFF In       | OFF Out   | OFF Total    | ON In       | ON Out    | ON Total    | In Reduction |
| --- | --------- | ------ | --------- | ------------ | --------- | ------------ | ----------- | --------- | ----------- | ------------ |
| 1   | R2S1      | simple | PASS/PASS | 271.9k       | 407       | 272.3k       | 23.0k       | 545       | 23.6k       | 92%          |
| 2   | R2S2      | simple | PASS/PASS | 271.5k       | 328       | 271.9k       | 22.2k       | 519       | 22.7k       | 92%          |
| 3   | R2S3      | simple | PASS/PASS | 270.3k       | 155       | 270.5k       | 40.7k       | 579       | 41.3k       | 85%          |
| 4   | R2S4      | simple | PASS/PASS | 407.3k       | 560       | 407.8k       | 33.1k       | 649       | 33.8k       | 92%          |
| 5   | R2S5      | simple | PASS/PASS | 409.6k       | 368       | 410.0k       | 54.1k       | 825       | 55.0k       | 87%          |
| 6   | R2S6      | simple | PASS/PASS | 270.3k       | 100       | 270.4k       | 18.9k       | 235       | 19.1k       | 93%          |
| 7   | R2S7      | simple | PASS/PASS | 270.4k       | 66        | 270.4k       | 19.5k       | 317       | 19.8k       | 93%          |
| 8   | R2S8      | simple | PASS/PASS | 270.4k       | 79        | 270.5k       | 19.4k       | 212       | 19.6k       | 93%          |
| 9   | R2S9      | simple | PASS/PASS | 270.3k       | 114       | 270.4k       | 18.8k       | 272       | 19.1k       | 93%          |
| 10  | R2S10     | simple | PASS/PASS | 270.3k       | 165       | 270.5k       | 18.7k       | 362       | 19.1k       | 93%          |
| 11  | R2S11     | simple | PASS/PASS | 270.9k       | 405       | 271.3k       | 21.3k       | 570       | 21.9k       | 92%          |
| 12  | R2S12     | simple | PASS/PASS | 270.3k       | 149       | 270.4k       | 18.9k       | 384       | 19.3k       | 93%          |
| 13  | R2S13     | simple | PASS/PASS | 272.1k       | 101       | 272.2k       | 22.8k       | 322       | 23.2k       | 92%          |
| 14  | R2S14     | simple | PASS/PASS | 274.7k       | 418       | 275.2k       | 38.6k       | 937       | 39.6k       | 86%          |
| 15  | R2S15     | simple | PASS/PASS | 271.2k       | 429       | 271.6k       | 22.4k       | 528       | 23.0k       | 92%          |
| 16  | R2S16     | simple | PASS/PASS | 270.9k       | 299       | 271.2k       | 21.0k       | 443       | 21.5k       | 92%          |
| 17  | R2S17     | simple | PASS/PASS | 271.0k       | 252       | 271.2k       | 21.5k       | 355       | 21.9k       | 92%          |
| 18  | R2S18     | simple | PASS/PASS | 405.9k       | 339       | 406.2k       | 19.2k       | 536       | 19.7k       | 95%          |
| 19  | R2S19     | simple | PASS/PASS | 405.6k       | 238       | 405.8k       | 31.4k       | 517       | 31.9k       | 92%          |
| 20  | R2S20     | simple | PASS/PASS | 680.0k       | 958       | 681.0k       | 42.3k       | 1.1k      | 43.5k       | 94%          |
| 21  | R2S21     | simple | PASS/PASS | 271.5k       | 153       | 271.7k       | 16.7k       | 221       | 16.9k       | 94%          |
| 22  | R2S22     | simple | PASS/PASS | 270.9k       | 335       | 271.2k       | 20.5k       | 691       | 21.2k       | 92%          |
| 23  | R2S23     | simple | PASS/PASS | 406.1k       | 471       | 406.6k       | 26.7k       | 743       | 27.5k       | 93%          |
| 24  | R2S24     | simple | PASS/PASS | 270.9k       | 127       | 271.0k       | 21.5k       | 260       | 21.8k       | 92%          |
| 25  | R2S25     | simple | PASS/PASS | 270.3k       | 264       | 270.6k       | 18.9k       | 526       | 19.5k       | 93%          |
| 26  | R2S26     | simple | PASS/PASS | 405.6k       | 345       | 406.0k       | 26.7k       | 827       | 27.6k       | 93%          |
| 27  | R2S27     | simple | PASS/PASS | 270.2k       | 145       | 270.4k       | 18.9k       | 558       | 19.5k       | 93%          |
| 28  | R2M1      | medium | PASS/PASS | 685.8k       | 1.2k      | 687.0k       | 37.7k       | 1.1k      | 38.7k       | 95%          |
| 29  | R2M2      | medium | PASS/PASS | 689.9k       | 1.0k      | 690.9k       | 76.0k       | 2.2k      | 78.2k       | 89%          |
| 30  | R2M3      | medium | PASS/PASS | 677.3k       | 473       | 677.8k       | 44.1k       | 1.5k      | 45.6k       | 93%          |
| 31  | R2M4      | medium | PASS/PASS | 541.6k       | 437       | 542.1k       | 17.7k       | 872       | 18.6k       | 97%          |
| 32  | R2M5      | medium | PASS/PASS | 814.6k       | 1.0k      | 815.6k       | 18.7k       | 1.1k      | 19.8k       | 98%          |
| 33  | R2M6      | medium | FAIL/PASS | 414.7k       | 8.3k      | 423.0k       | 90.5k       | 3.3k      | 93.7k       | 78%          |
| 34  | R2M7      | medium | PASS/PASS | 544.9k       | 737       | 545.6k       | 21.5k       | 706       | 22.2k       | 96%          |
| 35  | R2M8      | medium | PASS/PASS | 546.0k       | 1.3k      | 547.3k       | 43.8k       | 1.5k      | 45.2k       | 92%          |
| 36  | R2M9      | medium | PASS/PASS | 822.7k       | 1.7k      | 824.4k       | 102.9k      | 1.5k      | 104.4k      | 87%          |
| 37  | R2M10     | medium | PASS/PASS | 542.1k       | 645       | 542.7k       | 28.0k       | 938       | 28.9k       | 95%          |
| 38  | R2M11     | medium | PASS/PASS | 414.4k       | 767       | 415.1k       | 55.2k       | 1.2k      | 56.4k       | 87%          |
| 39  | R2M12     | medium | PASS/PASS | 863.9k       | 1.6k      | 865.5k       | 40.7k       | 1.3k      | 42.0k       | 95%          |
| 40  | R2M13     | medium | PASS/PASS | 678.1k       | 652       | 678.7k       | 45.3k       | 1.3k      | 46.6k       | 93%          |
| 41  | R2M14     | medium | PASS/PASS | 545.1k       | 959       | 546.0k       | 63.7k       | 4.0k      | 67.7k       | 88%          |
| 42  | R2M15     | medium | PASS/PASS | 844.3k       | 977       | 845.3k       | 191.7k      | 2.0k      | 193.7k      | 77%          |
| 43  | R2M16     | medium | PASS/PASS | 678.4k       | 680       | 679.1k       | 41.8k       | 1.4k      | 43.1k       | 94%          |
| 44  | R2M17     | medium | PASS/PASS | 714.6k       | 777       | 715.4k       | 755.3k      | 1.2k      | 756.5k      | -6%          |
| 45  | R2M18     | medium | PASS/PASS | 270.4k       | 210       | 270.6k       | 28.6k       | 483       | 29.0k       | 89%          |
| 46  | R2M19     | medium | PASS/PASS | 680.6k       | 818       | 681.4k       | 30.1k       | 936       | 31.0k       | 96%          |
| 47  | R2M20     | medium | PASS/PASS | 541.4k       | 640       | 542.0k       | 26.6k       | 963       | 27.6k       | 95%          |
| 48  | R2M21     | medium | PASS/PASS | 1233.8k      | 1.8k      | 1235.6k      | 42.1k       | 1.2k      | 43.2k       | 97%          |
| 49  | R2M22     | medium | PASS/PASS | 816.0k       | 928       | 817.0k       | 55.7k       | 1.6k      | 57.3k       | 93%          |
| 50  | R2M23     | medium | PASS/PASS | 685.5k       | 1.3k      | 686.8k       | 39.6k       | 1.5k      | 41.1k       | 94%          |
| 51  | R2M24     | medium | PASS/PASS | 827.1k       | 1.0k      | 828.1k       | 71.2k       | 1.6k      | 72.8k       | 91%          |
| 52  | R2M25     | medium | PASS/PASS | 406.2k       | 354       | 406.5k       | 26.9k       | 562       | 27.4k       | 93%          |
| 53  | R2H1      | medium | PASS/PASS | 900.8k       | 3.9k      | 904.7k       | 115.6k      | 6.1k      | 121.7k      | 87%          |
| 54  | R2H2      | medium | PASS/PASS | 816.1k       | 1.9k      | 818.0k       | 89.8k       | 2.8k      | 92.6k       | 89%          |
| 55  | R2H3      | medium | PASS/PASS | 880.3k       | 1.3k      | 881.6k       | 152.6k      | 2.7k      | 155.3k      | 83%          |
| 56  | R2H4      | medium | PASS/PASS | 977.9k       | 1.2k      | 979.0k       | 119.7k      | 1.4k      | 121.1k      | 88%          |
| 57  | R2H5      | medium | PASS/PASS | 544.7k       | 585       | 545.3k       | 32.7k       | 1.0k      | 33.7k       | 94%          |
| 58  | R2H6      | medium | PASS/PASS | 835.6k       | 4.5k      | 840.1k       | 1466.5k     | 4.0k      | 1470.5k     | -75%         |
| 59  | R2H7      | medium | PASS/PASS | 1169.1k      | 1.5k      | 1170.6k      | 435.6k      | 3.0k      | 438.6k      | 63%          |
| 60  | R2H8      | medium | PASS/PASS | 561.1k       | 786       | 561.9k       | 44.5k       | 921       | 45.5k       | 92%          |
| 61  | R2H9      | medium | PASS/PASS | 1097.1k      | 1.8k      | 1098.9k      | 88.2k       | 3.2k      | 91.3k       | 92%          |
| 62  | R2H10     | medium | PASS/PASS | 680.1k       | 816       | 680.9k       | 31.5k       | 1.2k      | 32.6k       | 95%          |
| 63  | R2H11     | medium | PASS/PASS | 1120.0k      | 4.7k      | 1124.7k      | 179.9k      | 5.3k      | 185.3k      | 84%          |
| 64  | R2H12     | medium | PASS/PASS | 683.0k       | 1.0k      | 684.0k       | 56.9k       | 1.2k      | 58.1k       | 92%          |
| 65  | R2H13     | medium | PASS/PASS | 407.3k       | 1.9k      | 409.2k       | 40.4k       | 3.8k      | 44.2k       | 90%          |
|     | **TOTAL** |        |           | **35692.9k** | **64.1k** | **35756.9k** | **5527.0k** | **86.7k** | **5613.7k** | **84.5%**    |


### Server-level vs Tool-level Binding

Server-level binding (one `.pyi` file per server) was also tested on Round 2: 65/65 pass, 5,803k total tokens, **83.8% reduction** — nearly identical to tool-level (84.5%). The choice between binding levels is use-case dependent; tool-level gives the model better tool discovery, while server-level has slightly fewer discovery round-trips.

### Observations

- **R2M6 (FAIL→PASS)**: Code Mode OFF failed because the model called `write_file` with only `{"path": "apollo_usage.txt"}`, omitting the required `content` parameter. It retried 60 times until timeout. Code Mode ON handled this correctly via Starlark — the `executeToolCode` sandbox allowed proper parameter construction, avoiding the hallucination.
- **R2M17 (-6%)** and **R2H6 (-75%)**: Two queries where Code Mode ON used MORE tokens than OFF, both due to large GitHub API responses inflating the Starlark execution context. These are known edge cases (see Known Issues in README).
- **Non-existent tool hallucination (OFF only)**: In Code Mode OFF, 6 queries called `LINEAR_GET_ALL_LINEAR_TEAMS` — a tool that doesn't exist. Code Mode ON avoided this entirely since tools are discovered via `listToolFiles` / `readToolFile`.

### Estimated Cost

Approximate LLM cost using Sonnet 4.6 pricing ($5/MTok input, $25/MTok output):


| Run                   | Input Cost | Output Cost | Total     |
| --------------------- | ---------- | ----------- | --------- |
| Round 1 Code Mode OFF | $99.34     | $4.70       | $104.04   |
| Round 1 Code Mode ON  | $41.53     | $4.53       | $46.06    |
| Round 2 Code Mode OFF | $178.46    | $1.60       | $180.07   |
| Round 2 Code Mode ON  | $27.63     | $2.17       | $29.80    |
| **R1+R2 Total**       |            |             | **~$360** |


---

# Round 3 — 508 tools, 16 servers

**Eval Set:** 65 queries across 16 MCP servers (508 tools)
**New servers:** Google Calendar (43), Google Drive (76), Google Docs (33), Calendly (51), Figma (52)

## Code Mode OFF


| Metric                 | Value        |
| ---------------------- | ------------ |
| Pass Rate              | 65/65 (100%) |
| Total Input Tokens     | 75,096,963   |
| Total Output Tokens    | 52,286       |
| Total Tokens           | 75,149,249   |
| Total Wall Clock       | 43.8 min     |
| Avg Input Tokens/Query | ~1,155k      |
| Est. Cost (Sonnet 4.6) | $376.79      |


### By Difficulty


| Difficulty | Queries | Pass  | Avg Input | Avg Latency |
| ---------- | ------- | ----- | --------- | ----------- |
| Simple     | 25      | 25/25 | 594.3k    | 22.3s       |
| Medium     | 40      | 40/40 | 1,506.0k  | 51.7s       |


### Observations (Code Mode OFF)

- **R3M36 token explosion (14.3M tokens, 7.2 min)**: The query "get SQLite schema, write to Google Doc" triggered a 46-turn loop. The model fetched all 8 table schemas correctly, then had to insert each table into the Google Doc cell-by-cell using low-level Google Docs API calls (insert table → get doc structure → find cell indices → insert text per cell). With ~550k tokens of tool schemas per turn, this compounded to 14.3M tokens for a single query — 19% of the entire run's token budget.
- **R3S20 & R3M2 — empty responses with pending tool calls**: R3S20 ("list Calendly events") ended after 1 turn with a pending `CALENDLY_GET_CURRENT_USER` call and no response text. R3M2 ("get Linear teams, write to file") called `LINEAR_GET_ALL_LINEAR_TEAMS` — the same non-existent tool hallucination from Round 2. Both still marked as "success" by Bifrost since the agent loop technically completed, but returned empty responses.
- **Apollo 403 (R3M3, R3M30)**: Apollo's people/organization search requires a master API key. The model correctly identified the error and wrote a report explaining the failure — queries passed but with partial data.
- **Token baseline**: With 508 tools, each API call sends ~275k tokens of tool schemas (vs ~135k at 251 tools, ~39k at 96 tools). A simple 2-turn query now consumes ~550k input tokens. Medium queries with 3-6 turns reach 825k–1.7M.

## Code Mode ON


| Metric                 | Value        |
| ---------------------- | ------------ |
| Pass Rate              | 65/65 (100%) |
| Total Input Tokens     | 5,415,646    |
| Total Output Tokens    | 90,037       |
| Total Tokens           | 5,505,683    |
| Total Wall Clock       | 36.3 min     |
| Avg Input Tokens/Query | ~83k         |
| Est. Cost (Sonnet 4.6) | $29.33       |


### By Difficulty


| Difficulty | Queries | Pass  | Avg Input | Avg Latency |
| ---------- | ------- | ----- | --------- | ----------- |
| Simple     | 25      | 25/25 | 36.5k     | 15.9s       |
| Medium     | 40      | 40/40 | 114.8k    | 44.5s       |


## Round 3 Comparison


| Metric                 | Code Mode OFF | Code Mode ON | Change     |
| ---------------------- | ------------- | ------------ | ---------- |
| Pass Rate              | 65/65 (100%)  | 65/65 (100%) | —          |
| Total Input Tokens     | 75,096,963    | 5,415,646    | **-92.8%** |
| Total Output Tokens    | 52,286        | 90,037       | +72.2%     |
| Total Tokens           | 75,149,249    | 5,505,683    | **-92.7%** |
| Total Wall Clock       | 43.8 min      | 36.3 min     | -17.1%     |
| Est. Cost (Sonnet 4.6) | $376.79       | $29.33       | **-92.2%** |


### By Difficulty


| Difficulty  | Queries | OFF Avg In  | ON Avg In | In Reduction | OFF Avg Lat | ON Avg Lat |
| ----------- | ------- | ----------- | --------- | ------------ | ----------- | ---------- |
| simple      | 25      | 594.3k      | 36.5k     | 94%          | 22.3s       | 15.9s      |
| medium      | 40      | 1,506.0k    | 114.8k    | 92%          | 51.7s       | 44.5s      |
| **Overall** | **65**  | **1155.3k** | **83.3k** | **92.8%**    | **40.4s**   | **33.5s**  |


### Per-Query Comparison


| #   | ID        | Diff   | OFF/ON    | OFF In       | OFF Out   | OFF Total    | ON In       | ON Out    | ON Total    | In Reduction |
| --- | --------- | ------ | --------- | ------------ | --------- | ------------ | ----------- | --------- | ----------- | ------------ |
| 1   | R3S1      | simple | PASS/PASS | 553.1k       | 400       | 553.5k       | 52.8k       | 666       | 53.5k       | 90%          |
| 2   | R3S2      | simple | PASS/PASS | 549.8k       | 66        | 549.9k       | 39.5k       | 393       | 39.9k       | 93%          |
| 3   | R3S3      | simple | PASS/PASS | 550.3k       | 403       | 550.7k       | 32.0k       | 589       | 32.6k       | 94%          |
| 4   | R3S4      | simple | PASS/PASS | 550.3k       | 330       | 550.7k       | 31.8k       | 394       | 32.2k       | 94%          |
| 5   | R3S5      | simple | PASS/PASS | 824.8k       | 268       | 825.0k       | 30.1k       | 517       | 30.6k       | 96%          |
| 6   | R3S6      | simple | PASS/PASS | 1100.1k      | 342       | 1100.4k      | 39.4k       | 448       | 39.8k       | 96%          |
| 7   | R3S7      | simple | PASS/PASS | 550.7k       | 152       | 550.8k       | 33.1k       | 324       | 33.4k       | 94%          |
| 8   | R3S8      | simple | PASS/PASS | 550.3k       | 396       | 550.7k       | 31.2k       | 657       | 31.9k       | 94%          |
| 9   | R3S9      | simple | PASS/PASS | 549.7k       | 212       | 549.9k       | 29.7k       | 546       | 30.2k       | 95%          |
| 10  | R3S10     | simple | PASS/PASS | 550.0k       | 196       | 550.2k       | 40.9k       | 634       | 41.5k       | 93%          |
| 11  | R3S11     | simple | PASS/PASS | 825.5k       | 472       | 825.9k       | 43.4k       | 802       | 44.2k       | 95%          |
| 12  | R3S12     | simple | PASS/PASS | 549.7k       | 94        | 549.8k       | 29.7k       | 284       | 30.0k       | 95%          |
| 13  | R3S13     | simple | PASS/PASS | 549.8k       | 214       | 550.0k       | 29.9k       | 414       | 30.4k       | 95%          |
| 14  | R3S14     | simple | PASS/PASS | 549.8k       | 148       | 549.9k       | 29.8k       | 283       | 30.1k       | 95%          |
| 15  | R3S15     | simple | PASS/PASS | 549.9k       | 268       | 550.2k       | 30.1k       | 410       | 30.6k       | 95%          |
| 16  | R3S16     | simple | PASS/PASS | 553.3k       | 272       | 553.6k       | 44.6k       | 403       | 45.0k       | 92%          |
| 17  | R3S17     | simple | PASS/PASS | 551.2k       | 336       | 551.6k       | 35.1k       | 762       | 35.8k       | 94%          |
| 18  | R3S18     | simple | PASS/PASS | 549.9k       | 289       | 550.2k       | 30.3k       | 474       | 30.8k       | 94%          |
| 19  | R3S19     | simple | PASS/PASS | 825.6k       | 332       | 825.9k       | 62.5k       | 854       | 63.3k       | 92%          |
| 20  | R3S20     | simple | PASS/PASS | 274.8k       | 48        | 274.8k       | 41.4k       | 518       | 41.9k       | 85%          |
| 21  | R3S21     | simple | PASS/PASS | 550.3k       | 134       | 550.4k       | 42.6k       | 645       | 43.2k       | 92%          |
| 22  | R3S22     | simple | PASS/PASS | 549.8k       | 193       | 550.0k       | 29.9k       | 390       | 30.3k       | 95%          |
| 23  | R3S23     | simple | PASS/PASS | 549.8k       | 164       | 550.0k       | 29.9k       | 444       | 30.3k       | 95%          |
| 24  | R3S24     | simple | PASS/PASS | 549.7k       | 118       | 549.8k       | 29.6k       | 274       | 29.9k       | 95%          |
| 25  | R3S25     | simple | PASS/PASS | 550.1k       | 272       | 550.4k       | 31.5k       | 413       | 31.9k       | 94%          |
| 26  | R3M1      | medium | PASS/PASS | 1110.9k      | 1.2k      | 1112.1k      | 57.0k       | 1.3k      | 58.3k       | 95%          |
| 27  | R3M2      | medium | PASS/PASS | 274.8k       | 46        | 274.8k       | 42.5k       | 1.3k      | 43.8k       | 85%          |
| 28  | R3M3      | medium | PASS/PASS | 825.3k       | 517       | 825.8k       | 479.9k      | 3.3k      | 483.3k      | 42%          |
| 29  | R3M4      | medium | PASS/PASS | 1103.2k      | 739       | 1103.9k      | 32.6k       | 802       | 33.4k       | 97%          |
| 30  | R3M5      | medium | PASS/PASS | 1104.9k      | 1.3k      | 1106.2k      | 59.3k       | 1.7k      | 61.0k       | 95%          |
| 31  | R3M6      | medium | PASS/PASS | 838.1k       | 706       | 838.8k       | 65.1k       | 1.1k      | 66.2k       | 92%          |
| 32  | R3M7      | medium | PASS/PASS | 549.8k       | 206       | 550.0k       | 106.9k      | 1.1k      | 108.0k      | 81%          |
| 33  | R3M8      | medium | PASS/PASS | 825.4k       | 373       | 825.8k       | 20.0k       | 683       | 20.7k       | 98%          |
| 34  | R3M9      | medium | PASS/PASS | 1101.8k      | 660       | 1102.4k      | 72.7k       | 1.8k      | 74.5k       | 93%          |
| 35  | R3M10     | medium | PASS/PASS | 826.6k       | 558       | 827.1k       | 56.3k       | 1.1k      | 57.3k       | 93%          |
| 36  | R3M11     | medium | PASS/PASS | 1376.2k      | 673       | 1376.9k      | 263.7k      | 5.0k      | 268.7k      | 81%          |
| 37  | R3M12     | medium | PASS/PASS | 825.5k       | 567       | 826.0k       | 42.1k       | 943       | 43.0k       | 95%          |
| 38  | R3M13     | medium | PASS/PASS | 1400.8k      | 901       | 1401.7k      | 179.2k      | 2.4k      | 181.6k      | 87%          |
| 39  | R3M14     | medium | PASS/PASS | 550.6k       | 342       | 551.0k       | 32.8k       | 534       | 33.4k       | 94%          |
| 40  | R3M15     | medium | PASS/PASS | 1654.5k      | 617       | 1655.1k      | 66.5k       | 1.2k      | 67.7k       | 96%          |
| 41  | R3M16     | medium | PASS/PASS | 1652.6k      | 685       | 1653.3k      | 65.1k       | 1.3k      | 66.4k       | 96%          |
| 42  | R3M17     | medium | PASS/PASS | 1685.5k      | 840       | 1686.3k      | 263.1k      | 3.6k      | 266.7k      | 84%          |
| 43  | R3M18     | medium | PASS/PASS | 825.4k       | 549       | 826.0k       | 51.3k       | 1.2k      | 52.5k       | 94%          |
| 44  | R3M19     | medium | PASS/PASS | 1662.2k      | 1.1k      | 1663.3k      | 91.0k       | 1.6k      | 92.6k       | 95%          |
| 45  | R3M20     | medium | PASS/PASS | 1661.1k      | 1.2k      | 1662.4k      | 76.4k       | 1.7k      | 78.1k       | 95%          |
| 46  | R3M21     | medium | PASS/PASS | 1101.8k      | 901       | 1102.7k      | 79.1k       | 2.6k      | 81.7k       | 93%          |
| 47  | R3M22     | medium | PASS/PASS | 1377.4k      | 581       | 1378.0k      | 68.6k       | 1.2k      | 69.8k       | 95%          |
| 48  | R3M23     | medium | PASS/PASS | 1377.4k      | 583       | 1378.0k      | 55.0k       | 1.1k      | 56.1k       | 96%          |
| 49  | R3M24     | medium | PASS/PASS | 837.2k       | 622       | 837.8k       | 63.4k       | 1.0k      | 64.4k       | 92%          |
| 50  | R3M25     | medium | PASS/PASS | 1662.3k      | 1.3k      | 1663.6k      | 79.7k       | 1.7k      | 81.4k       | 95%          |
| 51  | R3M26     | medium | PASS/PASS | 1656.8k      | 1.5k      | 1658.3k      | 82.5k       | 2.5k      | 85.1k       | 95%          |
| 52  | R3M27     | medium | PASS/PASS | 825.2k       | 436       | 825.7k       | 41.7k       | 755       | 42.5k       | 95%          |
| 53  | R3M28     | medium | PASS/PASS | 827.4k       | 741       | 828.1k       | 129.1k      | 2.4k      | 131.5k      | 84%          |
| 54  | R3M29     | medium | PASS/PASS | 1655.3k      | 640       | 1656.0k      | 83.9k       | 1.4k      | 85.3k       | 95%          |
| 55  | R3M30     | medium | PASS/PASS | 1378.3k      | 1.5k      | 1379.8k      | 68.9k       | 2.0k      | 70.8k       | 95%          |
| 56  | R3M31     | medium | PASS/PASS | 1654.7k      | 1.3k      | 1656.0k      | 87.1k       | 3.3k      | 90.4k       | 95%          |
| 57  | R3M32     | medium | PASS/PASS | 1665.0k      | 2.3k      | 1667.3k      | 131.3k      | 7.4k      | 138.7k      | 92%          |
| 58  | R3M33     | medium | PASS/PASS | 1101.6k      | 666       | 1102.2k      | 70.1k       | 1.2k      | 71.2k       | 94%          |
| 59  | R3M34     | medium | PASS/PASS | 827.4k       | 754       | 828.1k       | 52.0k       | 961       | 53.0k       | 94%          |
| 60  | R3M35     | medium | PASS/PASS | 1203.6k      | 681       | 1204.3k      | 772.6k      | 1.1k      | 773.7k      | 36%          |
| 61  | R3M36     | medium | PASS/PASS | 14262.8k     | 14.1k     | 14276.9k     | 240.3k      | 5.1k      | 245.5k      | 98%          |
| 62  | R3M37     | medium | PASS/PASS | 1100.7k      | 816       | 1101.5k      | 62.4k       | 1.2k      | 63.6k       | 94%          |
| 63  | R3M38     | medium | PASS/PASS | 1380.6k      | 1.5k      | 1382.0k      | 96.6k       | 4.7k      | 101.2k      | 93%          |
| 64  | R3M39     | medium | PASS/PASS | 1656.2k      | 732       | 1657.0k      | 70.7k       | 1.3k      | 72.0k       | 96%          |
| 65  | R3M40     | medium | PASS/PASS | 831.8k       | 709       | 832.5k       | 56.1k       | 1.3k      | 57.3k       | 93%          |
|     | **TOTAL** |        |           | **75097.0k** | **52.3k** | **75149.2k** | **5415.6k** | **90.0k** | **5505.7k** | **92.8%**    |


### Observations

- **R3M36 (14.3M → 245k tokens)**: The SQLite schema → Google Doc query dropped from 14.3M to 245k tokens — a **98.3% reduction** for a single query. In Code Mode OFF, the model needed 46 turns of cell-by-cell Google Doc manipulation (each turn sending ~275k of tool schemas). In Code Mode ON, the model wrote Starlark code to batch the work, reducing to 29 turns with only 4 meta-tools per turn.
- **R3M35 (773k tokens)**: GitHub issue search for `modelcontextprotocol/specification` was the highest Code Mode ON query. The `search_issues` API returned an error, so the model fell back to `list_issues` and paginated — the large response payloads inflated the context.

### Estimated Cost


| Run                   | Input Cost | Output Cost | Total   |
| --------------------- | ---------- | ----------- | ------- |
| Round 3 Code Mode OFF | $375.48    | $1.31       | $376.79 |
| Round 3 Code Mode ON  | $27.08     | $2.25       | $29.33  |


