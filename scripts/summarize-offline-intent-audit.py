"""Freeze and verify a post-hoc review of saved runs. No network/model calls."""
import argparse
import collections
import gzip
import hashlib
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
WORK = ROOT / 'eval/reports/offline-audit-20261009-1791554983597627200'
OUT = ROOT / 'eval/reviews/2026-10-09-intent-offline-audit'
DRAFT = ROOT / 'eval/annotations/2026-10-09-intent-contract-v1/cases.json'
ARMS = ('nojev', 'hint', 'fast')
VERDICTS = ('pass', 'fail', 'unverifiable', 'service_failure')
PARTS = ('reads_account', 'writes', 'catalog_other', 'adversarial')

def read(path):
    return json.loads(path.read_text(encoding='utf-8'))

def sha(raw):
    return hashlib.sha256(raw).hexdigest()

def evidence_path(relative):
    return ROOT / str(relative).replace('\\', '/')

def save(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2)+'\n', encoding='utf-8')

def stats(rows, field):
    counts = collections.Counter(r[field] for r in rows)
    total = len(rows)
    decidable = counts['pass'] + counts['fail']
    return {'total': total, **{k: counts[k] for k in VERDICTS},
            'confirmed_correct_percent_all': round(counts['pass']/total*100, 2) if total else None,
            'decidable_coverage_percent': round(decidable/total*100, 2) if total else None,
            'correct_percent_decidable': round(counts['pass']/decidable*100, 2) if decidable else None}

def summary(rows):
    result = {'ai_reviewed_cases': len(rows), 'human_reviewed_cases': 0,
              'new_model_calls': 0, 'new_benchmark_runs': 0,
              'annotation_changed_any': sum(r['review']['annotation_changed'] for r in rows),
              'expected_action_changed': sum(r['draft_action'] != r['review']['expected_action'] for r in rows),
              'expected_actions': dict(collections.Counter(r['review']['expected_action'] for r in rows)),
              'full_task_accuracy': None, 'numeric_truth_accuracy': None, 'groups': {}}
    groups = {'all': rows, 'normal': [r for r in rows if not r['id'].startswith('adv-')],
              'adversarial': [r for r in rows if r['id'].startswith('adv-')]}
    groups.update({k: [r for r in rows if r['id'].rsplit('-', 1)[0] == k]
                   for k in sorted({r['id'].rsplit('-', 1)[0] for r in rows})})
    for group, selected in groups.items():
        result['groups'][group] = {arm: {kind: stats([r['review']['arms'][arm] for r in selected], kind+'_verdict')
                                               for kind in ('action', 'reply')} for arm in ARMS}
    result['paired'] = {}
    for kind in ('action', 'reply'):
        common = [r for r in groups['normal'] if all(r['review']['arms'][a][kind+'_verdict'] in ('pass','fail') for a in ARMS)]
        result['paired'][kind] = {'normal_common_ids': [r['id'] for r in common],
                                 'excluded_normal_ids': [r['id'] for r in groups['normal'] if r not in common],
                                 'arms': {a: stats([r['review']['arms'][a] for r in common], kind+'_verdict') for a in ARMS}}
    return result

def build():
    if OUT.exists():
        raise SystemExit('Refusing to overwrite frozen review')
    sources = read(WORK/'sources.json')
    drafts = {r['id']: r for r in read(DRAFT)}
    predictions = {}
    for arm, source in sources.items():
        raw = (ROOT/source['path']).read_bytes()
        assert sha(raw) == source['compressed_sha256']
        plain = gzip.decompress(raw)
        assert sha(plain) == source['original_sha256']
        report = json.loads(plain)
        predictions[arm] = {r['Sample']['id']: r for r in report['Preds']+report['AdvPreds']}
        assert len(predictions[arm]) == 1220 and set(predictions[arm]) == set(drafts)
    reviews = [r for part in PARTS for r in read(WORK/(part+'-review.json'))]
    assert len(reviews) == 1220 and len({r['id'] for r in reviews}) == 1220
    assert {r['id'] for r in reviews} == set(drafts)
    rows = []
    for review in sorted(reviews, key=lambda r: r['id']):
        cid = review['id']
        assert review['expected_reply_requirements'] and review['annotation_reason']
        assert review['expected_action'] in ('read','clarify','consult','preview','respond','reject','limited')
        assert set(review['arms']) == set(ARMS)
        for arm in ARMS:
            pred, verdict = predictions[arm][cid], review['arms'][arm]
            assert pred['Sample']['text'] == drafts[cid]['original']['text']
            for kind in ('action','reply'):
                assert verdict[kind+'_verdict'] in VERDICTS and verdict[kind+'_reason']
                assert (verdict[kind+'_verdict'] == 'service_failure') == (pred['HTTPCode'] != 200 or bool(pred.get('Err'))), (cid, arm)
            assert verdict['full_task_verified'] is False and verdict['numeric_truth_verified'] is False
        rows.append({'id': cid, 'original': drafts[cid]['original'], 'draft_action': drafts[cid]['action'],
                     'status': 'ai_posthoc_review_not_blind_gold', 'review': review,
                     'historical': {a: predictions[a][cid] for a in ARMS}})
    totals = summary(rows)
    assert [totals['groups']['all'][a]['action']['service_failure'] for a in ARMS] == [97,107,97]
    OUT.mkdir(parents=True)
    save(OUT/'reviews.json', rows)
    save(OUT/'summary.json', totals)
    save(OUT/'sources.json', sources)
    (OUT/'draft-cases.json').write_bytes(DRAFT.read_bytes())
    for arm, source in sources.items():
        path = OUT/'raw'/(arm+'.json.gz')
        path.parent.mkdir(exist_ok=True)
        path.write_bytes((ROOT/source['path']).read_bytes())
    (OUT/'METHOD.md').write_bytes((WORK/'method.md').read_bytes())
    lines = ['# 1220条历史结果离线审核', '',
             '全部1220条原话及三组历史回复已进行AI逐条审核，形成3660条判定。没有调用模型或重跑评测。', '',
             '这是事后AI审核，审读时看到了历史输出，存在标注偏差；人工独立盲审数量为0，不能称正式金标。', '',
             '来源固定为2026-10-08 revised-full三组、deepseek-flash；不是最新网关模型成绩。原始结果和上一版规则草稿保持原样。', '',
             '## 全部1220条首轮动作', '',
             '|历史组|通过|明确失败|不可核验|服务失败|已确认正确/全部|可判定项正确率|',
             '|---|---:|---:|---:|---:|---:|---:|']
    for a in ARMS:
        s = totals['groups']['all'][a]['action']
        lines.append(f"|{a}|{s['pass']}|{s['fail']}|{s['unverifiable']}|{s['service_failure']}|{s['confirmed_correct_percent_all']}%|{s['correct_percent_decidable']}% ({s['pass']}/{s['pass']+s['fail']})|")
    lines += ['', '通过指首轮合理查询、必要追问、咨询说明、生成待确认预览或合理拒绝；不代表完成了多轮任务。服务失败保留在总分母，单列而不说成意图错误。合理追问不扣分；不必要追问、漏诉求和虚假确认状态扣分。', '',
              '## 回答证据', '', '|历史组|通过|明确失败|不可核验|服务失败|', '|---|---:|---:|---:|---:|']
    for a in ARMS:
        s = totals['groups']['all'][a]['reply']
        lines.append(f"|{a}|{s['pass']}|{s['fail']}|{s['unverifiable']}|{s['service_failure']}|")
    lines += ['', '余额、套餐费用和日期等缺少当时独立工具返回及账户快照，不能因为回复看起来合理就证明真实正确。明确矛盾仍判失败。1220条完整任务完成率与数值真值正确率均无法计算。此前另有24条带状态校验任务三组均24/24，该小样本不能替代这里的1220条。', '',
              '## 配对比较', '', '|指标（普通1200条三组共同可判定）|共同样本|nojev|hint|fast|', '|---|---:|---:|---:|---:|']
    for kind in ('action','reply'):
        paired = totals['paired'][kind]
        lines.append('|'+kind+'|'+str(len(paired['normal_common_ids']))+'|'+'|'.join(str(paired['arms'][a]['correct_percent_decidable'])+'%' for a in ARMS)+'|')
    lines += ['', '## 各类首轮动作：已确认正确/类内全部样本', '',
              '|类别|样本数|nojev|hint|fast|', '|---|---:|---:|---:|---:|']
    for category in sorted({r['id'].rsplit('-', 1)[0] for r in rows}):
        group = totals['groups'][category]
        lines.append('|'+category+'|'+str(group['nojev']['action']['total'])+'|'+'|'.join(
            f"{group[a]['action']['pass']}/{group[a]['action']['total']} ({group[a]['action']['confirmed_correct_percent_all']}%)" for a in ARMS)+'|')
    lines += ['', '首轮动作判定接受合理查询后追问，工具选择合理也可能同时存在回答错误。动作通过率不能称为回答准确率或完整任务成功率。', '',
              '上述比较仍是历史组合实验；hint、fast包含提示/安全路径差异，不能将收益全部归因于JEV高置信度直达。审核不会改变旧运行的延迟、token或成本；新边界未重跑，因此没有新的速度/成本结果。', '',
              f"标注动作类别修订 {totals['expected_action_changed']} 条；连同允许工具等补充共 {totals['annotation_changed_any']} 条。这不是原标注错误率。", '',
              '逐条原话、期望动作、期望回答要求、判定理由和历史证据见 [reviews.json](reviews.json)；各类/普通/对抗统计及配对ID见 [summary.json](summary.json)。', '',
              '来源哈希见 [sources.json](sources.json)，原始压缩结果在 raw/；原规则草稿保存为 draft-cases.json。审核方法见 [METHOD.md](METHOD.md)。', '',
              '校验：`python scripts/summarize-offline-intent-audit.py verify`。']
    (OUT/'README.md').write_text('\n'.join(lines)+'\n', encoding='utf-8')
    save(OUT/'archive-manifest.json', {'generator_sha256': sha(Path(__file__).read_bytes()),
          'files': [{'path': p.relative_to(OUT).as_posix(), 'sha256': sha(p.read_bytes())} for p in sorted(OUT.rglob('*')) if p.is_file()]})
    verify()

def verify():
    manifest = read(OUT/'archive-manifest.json')
    assert sha(Path(__file__).read_bytes()) == manifest['generator_sha256']
    assert {p.relative_to(OUT).as_posix() for p in OUT.rglob('*') if p.is_file() and p.name != 'archive-manifest.json'} == {f['path'] for f in manifest['files']}
    for f in manifest['files']:
        assert sha((OUT/f['path']).read_bytes()) == f['sha256']
    rows = read(OUT/'reviews.json')
    assert len(rows) == len({r['id'] for r in rows}) == 1220
    assert summary(rows) == read(OUT/'summary.json')
    assert (OUT/'draft-cases.json').read_bytes() == DRAFT.read_bytes()
    for arm, source in read(OUT/'sources.json').items():
        raw = (OUT/'raw'/(arm+'.json.gz')).read_bytes()
        assert raw == evidence_path(source['path']).read_bytes()
        assert sha(raw) == source['compressed_sha256'] and sha(gzip.decompress(raw)) == source['original_sha256']
        report = json.loads(gzip.decompress(raw))
        assert {p['Sample']['id']: p for p in report['Preds']+report['AdvPreds']} == {r['id']: r['historical'][arm] for r in rows}
    print('PASS: 1220 unique reviews, 3660 verdicts, recomputed counts, original bytes and all SHA256 verified. Zero model/benchmark calls.')

if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('command', choices=('build','verify'))
    (build if parser.parse_args().command == 'build' else verify)()
