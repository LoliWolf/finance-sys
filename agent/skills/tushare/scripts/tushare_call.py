#!/usr/bin/env python3
"""Generic CLI caller for Tushare Pro interfaces.

The token is intentionally accepted only from --token. This script does not
read environment variables, local Tushare config, or any other implicit
credential source.
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path
from typing import Any, Dict, Iterable, List, Optional


CATALOG_PATH = Path(__file__).resolve().parents[1] / "references" / "数据接口.md"


def clean_cell(value: str) -> str:
    value = value.replace("<br />", " ").replace("<br/>", " ")
    value = value.replace("**", "").replace("\\~", "~")
    return re.sub(r"\s+", " ", value).strip()


def load_catalog(path: Path = CATALOG_PATH) -> List[Dict[str, str]]:
    if not path.exists():
        return []

    row_pattern = re.compile(
        r"^\|\s*\[([A-Za-z0-9_]+)\]\(([^)]+)\)\s*"
        r"\|\s*([^|]*)\|\s*([^|]*)\|\s*(.*?)\s*\|?\s*$"
    )
    rows: List[Dict[str, str]] = []
    for line in path.read_text(encoding="utf-8").splitlines():
        match = row_pattern.match(line)
        if not match:
            continue
        api, url, title, category, description = match.groups()
        rows.append(
            {
                "api": api.strip(),
                "title": clean_cell(title),
                "category": clean_cell(category),
                "description": clean_cell(description),
                "url": url.strip(),
            }
        )
    return rows


def catalog_names(rows: Iterable[Dict[str, str]]) -> List[str]:
    names = sorted({row["api"] for row in rows})
    return names


def print_catalog(rows: List[Dict[str, str]], api: Optional[str] = None) -> int:
    filtered = [row for row in rows if api is None or row["api"] == api]
    if api and not filtered:
        print(f"No interface named {api!r} found in the interface catalog.", file=sys.stderr)
        return 1

    print("api\ttitle\tcategory\tdoc_url")
    for row in filtered:
        print(f"{row['api']}\t{row['title']}\t{row['category']}\t{row['url']}")
    return 0


def parse_param(raw: str) -> Dict[str, str]:
    if "=" not in raw:
        raise argparse.ArgumentTypeError("--param must use KEY=VALUE")
    key, value = raw.split("=", 1)
    key = key.strip()
    if not key:
        raise argparse.ArgumentTypeError("--param key cannot be empty")
    return {key: value}


def read_json_arg(raw: str) -> Dict[str, Any]:
    source = raw
    if raw.startswith("@"):
        path = Path(raw[1:])
        source = path.read_text(encoding="utf-8")
    data = json.loads(source)
    if not isinstance(data, dict):
        raise ValueError("--params-json must be a JSON object")
    return data


def build_params(args: argparse.Namespace) -> Dict[str, Any]:
    params: Dict[str, Any] = {}

    for raw in args.params_json or []:
        params.update(read_json_arg(raw))

    for item in args.param or []:
        params.update(item)

    return params


def infer_format(output: Optional[str], explicit_format: Optional[str]) -> str:
    if explicit_format:
        return explicit_format
    if output:
        suffix = Path(output).suffix.lower()
        if suffix == ".csv":
            return "csv"
        if suffix == ".json":
            return "json"
        if suffix in {".jsonl", ".ndjson"}:
            return "jsonl"
    return "table"


def call_tushare(args: argparse.Namespace, params: Dict[str, Any]):
    try:
        import tushare as ts
    except ImportError as exc:
        raise RuntimeError(
            "The 'tushare' package is not installed. Run: python -m pip install tushare"
        ) from exc

    api = args.api
    pro = ts.pro_api(args.token)

    if args.call_style == "pro-bar" or (args.call_style == "auto" and api == "pro_bar"):
        call_params = dict(params)
        call_params.setdefault("api", pro)
        if args.fields:
            call_params["fields"] = args.fields
        return ts.pro_bar(**call_params)

    call_params = dict(params)
    if args.fields:
        call_params["fields"] = args.fields

    if args.call_style == "method":
        func = getattr(pro, api, None)
        if not callable(func):
            raise RuntimeError(f"Tushare Pro object has no callable method {api!r}")
        return func(**call_params)

    return pro.query(api, **call_params)


def select_fields_if_needed(df: Any, fields: Optional[str]) -> Any:
    if not fields or not hasattr(df, "columns"):
        return df

    requested = [item.strip() for item in fields.split(",") if item.strip()]
    if not requested:
        return df

    missing = [name for name in requested if name not in df.columns]
    if missing:
        raise RuntimeError(
            "Requested fields were not returned: " + ", ".join(missing)
        )
    return df.loc[:, requested]


def write_json_records(df: Any, stream: Any, indent: Optional[int] = None) -> None:
    text = df.to_json(orient="records", force_ascii=False, date_format="iso")
    data = json.loads(text)
    json.dump(data, stream, ensure_ascii=False, indent=indent)
    stream.write("\n")


def write_output(df: Any, args: argparse.Namespace) -> None:
    fmt = infer_format(args.output, args.format)

    if args.output:
        output = Path(args.output)
        output.parent.mkdir(parents=True, exist_ok=True)
        if fmt == "csv":
            df.to_csv(output, index=False, encoding=args.encoding)
        elif fmt == "json":
            with output.open("w", encoding="utf-8") as fh:
                write_json_records(df, fh, indent=2)
        elif fmt == "jsonl":
            df.to_json(
                output,
                orient="records",
                force_ascii=False,
                lines=True,
                date_format="iso",
            )
        elif fmt == "table":
            output.write_text(df.to_string(index=False), encoding="utf-8")
        else:
            raise RuntimeError(f"Unsupported output format: {fmt}")
        print(f"Wrote {len(df)} rows and {len(df.columns)} columns to {output}", file=sys.stderr)
        return

    visible = df if args.head == 0 else df.head(args.head)
    if fmt == "csv":
        sys.stdout.write(visible.to_csv(index=False))
    elif fmt == "json":
        write_json_records(visible, sys.stdout, indent=2)
    elif fmt == "jsonl":
        sys.stdout.write(
            visible.to_json(
                orient="records",
                force_ascii=False,
                lines=True,
                date_format="iso",
            )
        )
    elif fmt == "table":
        if len(visible) == 0:
            print("(empty result)")
        else:
            print(visible.to_string(index=False))
        if args.head and len(df) > len(visible):
            print(f"# showing first {len(visible)} of {len(df)} rows", file=sys.stderr)
    else:
        raise RuntimeError(f"Unsupported output format: {fmt}")


def make_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Call any interface listed in tushare/references/数据接口.md.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=(
            "Examples:\n"
            "  python tushare/scripts/tushare_call.py --list\n"
            "  python tushare/scripts/tushare_call.py --show daily\n"
            "  python tushare/scripts/tushare_call.py daily --token TOKEN "
            "--param ts_code=000001.SZ --param start_date=20240101 "
            "--param end_date=20240131 --fields ts_code,trade_date,open,close\n"
            "  python tushare/scripts/tushare_call.py stock_basic --token TOKEN "
            "--param list_status=L --fields ts_code,symbol,name,area,industry,list_date "
            "--output out/stock_basic.csv\n"
        ),
    )
    parser.add_argument("api", nargs="?", help="Tushare interface name, for example daily")
    parser.add_argument("--token", help="Tushare token. Required for data calls.")
    parser.add_argument(
        "--param",
        action="append",
        type=parse_param,
        metavar="KEY=VALUE",
        help="API parameter. Repeat for multiple parameters.",
    )
    parser.add_argument(
        "--params-json",
        action="append",
        metavar="JSON_OR_@FILE",
        help="Merge API parameters from a JSON object string or @file.",
    )
    parser.add_argument("--fields", help="Comma-separated return fields passed to Tushare.")
    parser.add_argument("-o", "--output", help="Output file path. Format is inferred by suffix.")
    parser.add_argument(
        "--format",
        choices=["table", "csv", "json", "jsonl"],
        help="Output format. Defaults to table on stdout or inferred from --output.",
    )
    parser.add_argument(
        "--encoding",
        default="utf-8-sig",
        help="CSV encoding for file output. Default: utf-8-sig.",
    )
    parser.add_argument(
        "--head",
        type=int,
        default=20,
        help="Rows printed to stdout when --output is omitted. Use 0 for all rows.",
    )
    parser.add_argument(
        "--call-style",
        choices=["auto", "query", "method", "pro-bar"],
        default="auto",
        help="Tushare SDK call style. auto uses pro_bar for pro_bar and pro.query otherwise.",
    )
    parser.add_argument(
        "--allow-unknown",
        action="store_true",
        help="Allow an api name not listed in references/数据接口.md.",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Print the resolved api and parameters without importing Tushare or calling network.",
    )
    parser.add_argument(
        "--list",
        "--list-interfaces",
        dest="list_interfaces",
        action="store_true",
        help="List interfaces parsed from references/数据接口.md.",
    )
    parser.add_argument(
        "--show",
        "--show-interface",
        dest="show_interface",
        metavar="API",
        help="Show catalog rows for one interface.",
    )
    return parser


def main(argv: Optional[List[str]] = None) -> int:
    parser = make_parser()
    args = parser.parse_args(argv)
    rows = load_catalog()

    if args.list_interfaces:
        return print_catalog(rows)

    if args.show_interface:
        return print_catalog(rows, args.show_interface)

    if not args.api:
        parser.error("api is required unless --list or --show is used")

    names = catalog_names(rows)
    if names and args.api not in names and not args.allow_unknown:
        print(
            f"Interface {args.api!r} is not listed in the interface catalog. "
            "Use --allow-unknown to call it anyway.",
            file=sys.stderr,
        )
        return 2

    try:
        params = build_params(args)
    except Exception as exc:
        print(f"Failed to parse parameters: {exc}", file=sys.stderr)
        return 2

    if args.dry_run:
        payload = {"api": args.api, "params": params, "fields": args.fields}
        print(json.dumps(payload, ensure_ascii=False, indent=2))
        return 0

    if not args.token:
        parser.error("--token is required for data calls")

    try:
        result = call_tushare(args, params)
        if result is None:
            raise RuntimeError("Tushare returned None")
        result = select_fields_if_needed(result, args.fields if args.api == "pro_bar" else None)
        write_output(result, args)
    except Exception as exc:
        print(f"Tushare call failed for api={args.api!r}: {exc}", file=sys.stderr)
        if params:
            print(f"params={json.dumps(params, ensure_ascii=False)}", file=sys.stderr)
        return 1

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
