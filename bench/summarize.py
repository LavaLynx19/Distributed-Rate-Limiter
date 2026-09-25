"""Summarize a bench/docker-bench.sh results directory as Markdown tables.

Reports the median of the repetitions for each gateway/tool/mode, the
individual runs (so spread is visible), and mid-run CPU per container.
Standard library only.
"""

from __future__ import annotations

import json
import re
import statistics
import sys
from pathlib import Path

IDENTS = ("anon", "keyed")
UNITS_MS = {"us": 0.001, "ms": 1.0, "s": 1000.0}


def wrk_ms(value: str) -> float:
    number, unit = re.fullmatch(r"([\d.]+)(us|ms|s)", value).groups()
    return float(number) * UNITS_MS[unit]


def parse_autocannon(path: Path) -> dict:
    d = json.loads(path.read_text())
    lat = d["latency"]
    return {
        "rps": d["requests"]["average"],
        "p50": lat["p50"],
        "p99": lat["p99"],
        "max": lat["max"],
        "bad": d["non2xx"] + d["errors"] + d["timeouts"],
    }


def parse_wrk(path: Path) -> dict:
    text = path.read_text()
    pct = dict(re.findall(r"^\s+(50|99)%\s+(\S+)$", text, re.MULTILINE))
    bad = sum(int(n) for n in re.findall(r"Non-2xx or 3xx responses: (\d+)", text))
    errors = re.search(
        r"Socket errors: connect (\d+), read (\d+), write (\d+), timeout (\d+)", text
    )
    if errors:
        bad += sum(int(n) for n in errors.groups())
    return {
        "rps": float(re.search(r"Requests/sec:\s+([\d.]+)", text).group(1)),
        "p50": wrk_ms(pct["50"]),
        "p99": wrk_ms(pct["99"]),
        "max": wrk_ms(re.search(r"Latency\s+\S+\s+\S+\s+(\S+)", text).group(1)),
        "bad": bad,
    }


def parse_cpu(path: Path) -> dict[str, float]:
    cpu = {}
    for line in path.read_text().splitlines():
        name, pct = line.split()
        role = (
            "redis"
            if "redis" in name
            else "loadgen"
            if "loadgen" in name
            else "gateway"
        )
        cpu[role] = float(pct.rstrip("%"))
    return cpu


def main(results: Path) -> None:
    runs = {}
    for f in sorted(results.iterdir()):
        m = re.fullmatch(
            r"(node|go)-(autocannon|wrk)-(strict|loose)-(?:(anon|keyed)-)?(\d+)\.(json|txt)",
            f.name,
        )
        if not m:
            continue
        impl, tool, mode, ident, _, ext = m.groups()
        ident = ident or "anon"
        stats = parse_autocannon(f) if ext == "json" else parse_wrk(f)
        stats["cpu"] = parse_cpu(f.with_suffix(".cpu"))
        runs.setdefault((ident, tool, mode, impl), []).append(stats)

    print(f"# Docker benchmark — {results.name}\n")
    print("Median of repetitions. CPU is % of one vCPU, sampled mid-run.")
    print(
        "Identity: anon = client IP; keyed = registered API key (cached registry lookup).\n"
    )
    print(
        "| Identity | Tool | Mode | Gateway | Runs (RPS) | Median RPS | p50 | p99 | Max | Gateway CPU | Redis CPU | Loadgen CPU | Errors |"
    )
    print("|---|---|---|---|---|---|---|---|---|---|---|---|---|")
    medians = {}
    for ident in IDENTS:
        for tool in ("autocannon", "wrk"):
            for mode in ("strict", "loose"):
                for impl in ("node", "go"):
                    rs = runs.get((ident, tool, mode, impl))
                    if not rs:
                        continue
                    median_run = sorted(rs, key=lambda r: r["rps"])[len(rs) // 2]
                    med = statistics.median(r["rps"] for r in rs)
                    medians[(ident, tool, mode, impl)] = med
                    cpu = {
                        k: statistics.median(r["cpu"].get(k, 0) for r in rs)
                        for k in ("gateway", "redis", "loadgen")
                    }
                    each = " / ".join(f"{r['rps']:,.0f}" for r in rs)
                    print(
                        f"| {ident} | {tool} | {mode} | {impl} | {each} "
                        f"| **{med:,.0f}** | {median_run['p50']:g} ms | {median_run['p99']:g} ms | {median_run['max']:g} ms "
                        f"| {cpu['gateway']:.0f}% | {cpu['redis']:.0f}% | {cpu['loadgen']:.0f}% | {sum(r['bad'] for r in rs)} |"
                    )

    print(
        "\n| Identity | Tool | Mode | Go / Node | Node keyed / anon | Go keyed / anon |"
    )
    print("|---|---|---|---|---|---|")
    for ident in IDENTS:
        for tool in ("autocannon", "wrk"):
            for mode in ("strict", "loose"):
                node = medians.get((ident, tool, mode, "node"))
                go = medians.get((ident, tool, mode, "go"))
                if not (node and go):
                    continue
                cost = {}
                for impl in ("node", "go"):
                    anon = medians.get(("anon", tool, mode, impl))
                    keyed = medians.get(("keyed", tool, mode, impl))
                    cost[impl] = (
                        f"{keyed / anon:.2f}×"
                        if ident == "keyed" and anon and keyed
                        else "—"
                    )
                print(
                    f"| {ident} | {tool} | {mode} | {go / node:.2f}× | {cost['node']} | {cost['go']} |"
                )


if __name__ == "__main__":
    main(Path(sys.argv[1]))
