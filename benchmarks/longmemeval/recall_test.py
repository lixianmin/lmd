#!/usr/bin/env python3
"""LongMemEval recall test against LMD's indexed collection."""
import json, subprocess, sys, re, math, urllib.parse, time

d = json.load(open('/Users/xmli/.cache/lmd/benchmarks/longmemeval_s_cleaned.json'))
MODE = sys.argv[1] if len(sys.argv) > 1 else "query"
LIMIT = int(sys.argv[2]) if len(sys.argv) > 2 else 0
PORT = 12345

def search(query, mode, top_k=10):
    """Call LMD's search/query endpoint with retry on model loading."""
    url = f"http://localhost:{PORT}/{mode}"
    body = json.dumps({"query": query, "collection": "bench_full", "limit": top_k}).encode()
    for attempt in range(5):
        try:
            import urllib.request
            req = urllib.request.Request(url, data=body, headers={"Content-Type": "application/json"})
            resp = urllib.request.urlopen(req, timeout=120)
            data = json.loads(resp.read())
            hits = data.get("hits", [])
            if hits is not None:
                return hits
        except Exception as e:
            print(f"  search error (attempt {attempt+1}/5): {e}", file=sys.stderr)
            time.sleep(5)
    return []

def extract_session_id(path):
    """Extract session index from filename: q0001_s042.md → 41 (0-indexed)"""
    m = re.search(r'q(\d+)_s(\d+)\.md', path)
    if m:
        qi = int(m.group(1)) - 1
        si = int(m.group(2)) - 1
        return qi, si
    return -1, -1

def recall_at_k(retrieved, relevant, k):
    if not relevant:
        return 0
    found = sum(1 for r in retrieved[:k] if r in relevant)
    return found / len(relevant)

def ndcg_at_k(retrieved, relevant, k):
    if not relevant:
        return 0
    dcg = sum(1.0 / math.log2(i + 2) for i, r in enumerate(retrieved[:k]) if r in relevant)
    ideal = sum(1.0 / math.log2(i + 2) for i in range(min(len(relevant), k)))
    return dcg / ideal if ideal else 0

print(f"LongMemEval Recall Test — LMD /{MODE}")
print(f"Questions: {len(d)}, Mode: {MODE}")
print(f"{'='*60}")
print()

# Warm up embedding model
print("Warming up embedding model...", file=sys.stderr)
search("warmup", MODE, 1)
print("Model ready.", file=sys.stderr)
print()

r5_sum = r10_sum = ndcg_sum = 0.0
valid = 0
start = time.time()

current_q = -1
zipped = []

for qi, q in enumerate(d):
    if LIMIT > 0 and qi >= LIMIT:
        break

    relevant = set()
    for i, s in enumerate(q['haystack_sessions']):
        for t in s:
            if t.get('has_answer'):
                relevant.add(i)
                break
    if not relevant:
        continue

    # Search
    hits = search(q['question'], MODE, 30)

    # Map hits to session IDs
    retrieved = []
    seen = set()
    for h in hits:
        path = h.get('Path', h.get('path', ''))
        h_qi, si = extract_session_id(path)
        if h_qi != qi:
            continue  # skip sessions from other questions
        if si >= 0 and si not in seen:
            seen.add(si)
            retrieved.append(si)

    # Compute metrics
    # Note: if hit count is low, pad with empty results
    if len(retrieved) < 10:
        zipped.append((qi, len(retrieved), len(relevant), retrieved[:5]))

    r5 = recall_at_k(retrieved, relevant, 5)
    r10 = recall_at_k(retrieved, relevant, 10)
    n10 = ndcg_at_k(retrieved, relevant, 10)
    r5_sum += r5
    r10_sum += r10
    ndcg_sum += n10
    valid += 1

    current_q = qi
    if (qi + 1) % 50 == 0:
        elapsed = time.time() - start
        print(f"[{qi+1}/{len(d) if LIMIT == 0 else LIMIT}] R@5={r5_sum/valid*100:.1f}% ({elapsed:.0f}s)", file=sys.stderr, end='\r')

print(file=sys.stderr)
print()
print(f"{'Mode':10s} | Recall@5 | Recall@10 | NDCG@10 | Q")
print(f"{'-'*10}-|----------|-----------|--------|---")
print(f"{MODE:10s} |   {r5_sum/valid*100:5.1f}% |    {r10_sum/valid*100:5.1f}% |  {ndcg_sum/valid:6.3f} | {valid}")

if zipped:
    print(f"\n Low-recall questions ({len(zipped)} with <10 hits):")
    for qi, nhits, nrel, ret5 in zipped[:5]:
        print(f"  q{qi}: {nhits} hits, {nrel} relevant, ret5={ret5}")

print(f"\n Completed in {time.time()-start:.0f}s")
