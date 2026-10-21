"""Read-only production-model concurrency probe. Does not deploy or mutate Nacos/DB.

Run with agent/.venv/bin/python. Complete schema-valid responses, not first tokens,
count as successes. Results contain metadata only, never credentials or article text.
"""
from __future__ import annotations

import argparse
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass, field
import hashlib
import json
import math
from pathlib import Path
import threading
import time


def percentile(values, quantile):
    if not values:
        return None
    values = sorted(values)
    return values[max(0, math.ceil(len(values) * quantile) - 1)]


def classify_error(error, status=None):
    text = error.lower()
    if status == 429 or any(s in text for s in ('concurrency limit', 'too many concurrent', 'rate limit', 'http 429')):
        return 'capacity_rejection'
    if 'timed out' in text or 'deadline' in text:
        return 'timeout'
    if status in (401, 403):
        return 'authentication_or_permission'
    if 'stream' in text:
        return 'stream_failure'
    if 'invalid llm response' in text or 'validation error' in text:
        return 'invalid_output'
    return 'request_failure'


def assess(rows, target, peak):
    ok = sum(r['ok'] for r in rows)
    reasons = [r['error_kind'] for r in rows if not r['ok']]
    transport_failures = [r for r in reasons if r != 'invalid_output']
    return {
        'passed': bool(rows) and peak >= target and ok / len(rows) >= 0.95 and not transport_failures,
        'success': ok,
        'requests': len(rows),
        'success_rate': ok / len(rows) if rows else 0,
        'errors': {kind: reasons.count(kind) for kind in sorted(set(reasons))},
        'observed_http_peak': peak,
        'p95_seconds': percentile([r['seconds'] for r in rows], 0.95),
        'p95_first_token_seconds': percentile([r['first_token_seconds'] for r in rows if r['first_token_seconds'] is not None], 0.95),
    }


def saturated(previous, current):
    """Conservative indication of queueing rather than extra useful throughput."""
    old = previous.get('output_chars_per_second', 0)
    new = current.get('output_chars_per_second', 0)
    return (old > 0 and new < old * 1.10
            and current['p95_seconds'] > previous['p95_seconds'] * 1.6)


@dataclass
class Counter:
    lock: threading.Lock = field(default_factory=threading.Lock)
    active: int = 0
    peak: int = 0

    def enter(self):
        with self.lock:
            self.active += 1
            self.peak = max(self.peak, self.active)

    def leave(self):
        with self.lock:
            self.active -= 1


class ObservedResponse:
    def __init__(self, response, stats, started, secrets, redact):
        self.inner = response
        self.stats = stats
        self.started = started
        self.secrets = secrets
        self.redact = redact

    def __getattr__(self, name):
        return getattr(self.inner, name)

    def __enter__(self):
        self.inner.__enter__()
        return self

    def __exit__(self, *args):
        return self.inner.__exit__(*args)

    def readline(self, *args):
        line = self.inner.readline(*args)
        if line.startswith(b'data:'):
            try:
                item = json.loads(line[5:].strip())
                if isinstance(item, dict):
                    if item.get('error'):
                        self.stats['stream_error'] = self.redact(
                            json.dumps(item['error'], ensure_ascii=False), self.secrets)[:300]
                    for choice in item.get('choices', []):
                        delta = choice.get('delta') or {}
                        if delta.get('content') or delta.get('reasoning_content'):
                            if self.stats['first_token_seconds'] is None:
                                self.stats['first_token_seconds'] = time.monotonic() - self.started
            except (ValueError, TypeError, AttributeError):
                pass  # Production parser remains the authority; this is timing only.
        return line


def main():
    import sys
    root = Path(__file__).resolve().parents[1]
    sys.path[:0] = [str(root), str(root / 'agent')]
    import start_prod
    from app.config import _settings_from_nacos_config
    from app.llm_client import LLMClient, build_system_prompt
    from app.skills import load_instrument_resolution_skill
    from app import http_client

    parser = argparse.ArgumentParser()
    parser.add_argument('--ceiling', type=int, default=64)
    parser.add_argument('--waves', type=int, default=2)
    parser.add_argument('--levels', default='', help='explicit follow-up levels, e.g. 60,61; no repeated baseline')
    parser.add_argument('--dataset', default='tmp/september_sota/dataset.json')
    parser.add_argument('--output', required=True)
    args = parser.parse_args()
    explicit_levels = [int(v) for v in args.levels.split(',')] if args.levels else []
    if any(v < 1 or v > 128 for v in explicit_levels):
        raise ValueError('explicit levels must be 1..128')
    if not 10 <= args.ceiling <= 128 or not 2 <= args.waves <= 5:
        raise ValueError('ceiling must be 10..128; waves must be 2..5')
    out = (root / args.output).resolve()
    out.relative_to(root / 'tmp')
    out.mkdir(parents=True, exist_ok=False)
    cfg = start_prod.load_nacos(start_prod.nacos_address(root))
    settings = _settings_from_nacos_config(cfg)
    settings = settings.model_copy(deep=True)
    settings.llm.max_retries = 0
    if not settings.llm.enabled:
        raise RuntimeError('production model disabled')
    skill = load_instrument_resolution_skill()
    documents = json.loads((root / args.dataset).read_text())
    # Real, complete production text; no truncation or gold labels sent to the model.
    eligible = [d for d in documents if d.get('cleaned_text') and 350 <= len(d['cleaned_text']) <= 1600]
    eligible.sort(key=lambda d: hashlib.sha256(('capacity-v1|' + str(d['id'])).encode()).hexdigest())
    cases = eligible[:10]
    if len(cases) < 10:
        raise RuntimeError('need ten distinct real article inputs')
    manifest = {
        'model': settings.llm.model, 'config_version': cfg['meta']['config_version'],
        'temperature': settings.llm.temperature, 'stream': settings.llm.stream,
        'request_timeout_ms': settings.llm.timeout_ms, 'probe_retries': 0,
        'prompt_sha256': hashlib.sha256(build_system_prompt(skill, settings.llm.extraction_prompt).encode()).hexdigest(),
        'case_ids': [str(d['id']) for d in cases], 'case_characters': [len(d['cleaned_text']) for d in cases],
        'waves': args.waves, 'ceiling': args.ceiling,
        'explicit_levels': explicit_levels,
        'criterion': '>=95% complete schema-valid outputs; no transport/capacity failures; target HTTP overlap reached',
        'capacity_scope': 'measured client working concurrency on this fixed workload, not a vendor hardware limit',
        'writes_production_data': False, 'updates_nacos': False, 'deploys_services': False,
    }
    (out / 'manifest.json').write_text(json.dumps(manifest, ensure_ascii=False, indent=2))
    local = threading.local()
    original_open = http_client.open_url
    secrets = [settings.llm.api_key, *settings.llm.extra_headers.values()]

    def observed_open(request, timeout):
        response = original_open(request, timeout)
        local.stats['http_status'] = response.status
        return ObservedResponse(response, local.stats, local.started, secrets, http_client.redact_secrets)

    http_client.open_url = observed_open
    stages = []

    def stage(concurrency, label):
        counter = Counter()
        rows = []
        started_stage = time.monotonic()
        for wave in range(args.waves):
            barrier = threading.Barrier(concurrency)

            def one(index):
                doc = cases[(wave * concurrency + index) % len(cases)]
                chunks = doc['chunks_json']
                if isinstance(chunks, str):
                    chunks = json.loads(chunks)
                source = '\n\n'.join(c['text'] for c in chunks)
                stats = {'first_token_seconds': None, 'http_status': None, 'stream_error': ''}
                local.stats = stats
                barrier.wait(timeout=30)
                local.started = time.monotonic()
                counter.enter()
                result = {'document_id': str(doc['id']), 'wave': wave + 1, 'index': index, 'ok': False, 'output_chars': 0}
                try:
                    extraction = LLMClient(settings=settings).extract_document(source, 0, skill)
                    result.update(ok=True, intent_count=len(extraction.raw_intents),
                                  output_chars=len(json.dumps([i.model_dump() for i in extraction.raw_intents], ensure_ascii=False)))
                except Exception as exc:
                    error = http_client.redact_secrets(str(exc), secrets)[:400]
                    combined = error + ' ' + stats['stream_error']
                    result.update(error=error, error_kind=classify_error(combined, stats['http_status']))
                finally:
                    counter.leave()
                result.update(seconds=round(time.monotonic() - local.started, 3), **stats)
                return result

            with ThreadPoolExecutor(max_workers=concurrency) as pool:
                futures = [pool.submit(one, i) for i in range(concurrency)]
                for future in as_completed(futures):
                    row = future.result()
                    rows.append(row)
                    (out / f'{label}-results.json').write_text(json.dumps(rows, ensure_ascii=False, indent=2))
                    print(json.dumps({'stage': label, 'concurrency': concurrency, 'completed': len(rows),
                                      'planned': concurrency * args.waves, 'ok': row['ok'],
                                      'seconds': row['seconds'], 'error_kind': row.get('error_kind')}, ensure_ascii=False), flush=True)
            if any(r.get('error_kind') in ('capacity_rejection', 'authentication_or_permission') for r in rows):
                break  # Do not amplify explicit provider refusals with more waves.
        elapsed = time.monotonic() - started_stage
        result = {'label': label, 'concurrency': concurrency, **assess(rows, concurrency, counter.peak),
                  'elapsed_seconds': round(elapsed, 3),
                  'output_chars_per_second': sum(r['output_chars'] for r in rows if r['ok']) / elapsed,
                  'complete_requests_per_minute': sum(r['ok'] for r in rows) * 60 / elapsed}
        if len(rows) < concurrency * args.waves:
            result['passed'] = False
        stages.append(result)
        (out / 'stages.json').write_text(json.dumps(stages, ensure_ascii=False, indent=2))
        print('STAGE ' + json.dumps(result, ensure_ascii=False), flush=True)
        return result

    conclusion = {'allowed_to_proceed': False, 'recommended_concurrency': None, 'hard_limit_proven': False}
    try:
        if explicit_levels:
            for level in explicit_levels:
                result = stage(level, f'confirm-{level}')
                if not result['passed']:
                    conclusion.update(reason='explicit level failed', failed_or_saturated_level=level)
                    break
                conclusion.update(allowed_to_proceed=level >= 10, recommended_concurrency=level,
                                  reason='explicit levels completed; not a proven vendor hard limit')
            conclusion['stages'] = stages
            (out / 'conclusion.json').write_text(json.dumps(conclusion, ensure_ascii=False, indent=2))
            print('CONCLUSION ' + json.dumps(conclusion, ensure_ascii=False), flush=True)
            return
        baseline = stage(5, 'baseline-5')
        if baseline['errors'].get('capacity_rejection') or baseline['errors'].get('authentication_or_permission'):
            conclusion.update(reason='baseline explicitly refused; stop below required gate', stages=stages)
            (out / 'conclusion.json').write_text(json.dumps(conclusion, ensure_ascii=False, indent=2))
            return
        threshold = stage(10, 'threshold-10')
        if not threshold['passed']:
            conclusion.update(reason='10-concurrency gate failed; stop before deployment, backup or deletion',
                              observed_lower_working_level=5 if baseline['passed'] else None)
        elif baseline['passed'] and saturated(baseline, threshold):
            confirm = stage(10, 'confirm-saturation-10')
            if saturated(baseline, confirm):
                conclusion.update(reason='effective throughput saturated below 10; provider may be queueing', observed_lower_working_level=5)
            elif confirm['passed']:
                conclusion.update(allowed_to_proceed=True, recommended_concurrency=10, reason='10 gate confirmed')
        else:
            conclusion.update(allowed_to_proceed=True, recommended_concurrency=10, reason='10 gate passed')
        if conclusion['allowed_to_proceed']:
            good = threshold
            level = 20
            while good['concurrency'] < args.ceiling:
                level = min(level, args.ceiling)
                trial = stage(level, f'candidate-{level}')
                if not trial['passed'] or saturated(good, trial):
                    conclusion.update(reason='higher stage failed or only added queueing; last successful working level retained', failed_or_saturated_level=level)
                    break
                good = trial
                conclusion['recommended_concurrency'] = level
                if level == args.ceiling:
                    conclusion.update(reason='probe safety ceiling reached; tested lower bound, not proven vendor maximum', ceiling_reached=True)
                    break
                level *= 2
        conclusion['stages'] = stages
        (out / 'conclusion.json').write_text(json.dumps(conclusion, ensure_ascii=False, indent=2))
        print('CONCLUSION ' + json.dumps(conclusion, ensure_ascii=False), flush=True)
    finally:
        http_client.open_url = original_open


if __name__ == '__main__':
    main()
