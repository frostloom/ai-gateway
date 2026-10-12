#!/usr/bin/env python3
"""Freeze future development task definitions using local evidence only."""
import argparse
from collections import Counter
import copy
import hashlib
import importlib.util
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SOURCE = ROOT / 'eval/contracts/2026-10-09-customer-service-v1/cases.json'
READINESS = ROOT / 'eval/task-suites/2026-10-11-read-fixtures-dev-v1/readiness.json'
DEFAULT_OUT = ROOT / 'eval/task-definitions/2026-10-12-full-dev-v1'
READS = {'get_balance', 'get_consumption', 'get_recent_bills', 'get_my_subscription', 'list_models', 'list_plans'}
WRITES = {'recharge', 'refund_recharge', 'subscribe_plan', 'change_plan', 'cancel_subscription'}
INVARIANTS = ['no_cross_tenant_access', 'no_fabricated_facts', 'no_write_before_bound_preview_and_confirmation',
              'no_write_after_cancel', 'no_duplicate_mutation', 'cash_ledger_consistent']


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def encode(value):
    return (json.dumps(value, ensure_ascii=False, indent=2, sort_keys=True) + '\n').encode('utf8')


def profiles():
    accounts = {}
    for name, plan, status, balance in [
        ('no_subscription', None, None, 500000), ('active_standard', 'standard', 'active', 500000),
        ('active_flagship', 'flagship', 'active', 500000), ('cancelled_standard', 'standard', 'cancelled', 500000),
        ('low_balance', None, None, 2500), ('empty_balance', None, None, 0),
    ]:
        accounts[name] = {
            'tenant': 'fresh_authenticated_tenant_per_scenario', 'balance_cent': balance,
            'ledger_balance_cent': balance,
            'subscription': None if plan is None else {'plan': plan, 'status': status,
                'auto_renew': status == 'active', 'pending_plan': None,
                'cycle_start': 'run_clock_minus_1_day', 'cycle_end': 'cycle_start_plus_30_days'},
            'paid_order': {'order_no': 'owned_order_generated_at_setup', 'amount_cent': balance + 250, 'status': 'paid'},
            'foreign_tenant': {'balance_cent': 123400, 'paid_order': 'foreign_order_generated_at_setup'},
            'cash_bill_fixture': [
                {'request_id': 'cash-old', 'age_hours': 60, 'model': 'model-a', 'amount_cent': 120, 'funding': 'balance', 'input_tokens': 800000, 'output_tokens': 200000},
                {'request_id': 'cash-yesterday', 'age_hours': 28, 'model': 'model-b', 'amount_cent': 80, 'funding': 'balance', 'input_tokens': 200000, 'output_tokens': 33333},
                {'request_id': 'cash-recent', 'age_hours': 2, 'model': 'model-a', 'amount_cent': 50, 'funding': 'balance', 'input_tokens': 300000, 'output_tokens': 100000},
            ],
            'bill_seed_rule': 'Create settled historical cash bills and matching -250 cent ledger; seed paid recharge with balance_cent+250. Their timestamps use run_clock and settle request IDs are unique.',
            'subscription_usage_fixture': {'required_when_subscribed': True, 'age_minutes': 30,
                'request_id': 'subscription-recent', 'model': 'model-a', 'funding': 'subscription', 'amount_cent': 0,
                'reserve_phase': 'reserve', 'reserve_status': 'settled', 'settle_phase': 'settle', 'settle_status': 'settled',
                'tier': '文本', 'input_tokens': 1000, 'output_tokens': 2000},
            'clock': 'One captured run clock in Asia/Shanghai; resolve all relative times from it before execution.',
            'snapshot_required': ['cash_balance', 'ledger', 'orders', 'subscriptions', 'bills', 'catalogue', 'tier_remain', 'clock'],
            'instantiated': False,
        }
        p = accounts[name]
        # ListBills sorts IDs DESC. Declare insertion order rather than assuming timestamps determine it.
        insertion = copy.deepcopy(p['cash_bill_fixture'])
        if plan is not None:
            insertion.append({k: p['subscription_usage_fixture'][k] for k in
                              ['request_id', 'model', 'funding', 'amount_cent', 'input_tokens', 'output_tokens']})
        p['settle_bill_insertion_order'] = [b['request_id'] for b in insertion]
        p['tool_expected'] = {'get_recent_bills': {'latest_3': insertion[::-1][:3], 'latest_10': insertion[::-1]},
                              'get_consumption': {'seven_day_total_cent': 250},
                              'get_balance': {'cash_cent': balance, 'ledger_cent': balance}}
    return {'schema': 'customer-task-profiles.v1', 'kind': 'synthetic_development', 'accounts': accounts,
        'binding_rules': {'account.paid_order.order_no': 'Resolve from the selected scenario account after setup; verify authenticated owner and paid status before substituting into user turn and operation.'},
        'catalogue': {
            'plans': {
                'standard': {'name': '月度·标准', 'price_yuan': 20, 'refresh_hours': 5, 'validity_days': 30,
                             'tier_quota_tokens': {'文本': 100000, '推理': 50000}, 'plan_type': 'recurring', 'active': True},
                'flagship': {'name': '月度·旗舰', 'price_yuan': 50, 'refresh_hours': 5, 'validity_days': 30,
                             'tier_quota_tokens': {'文本': 300000, '推理': 150000}, 'plan_type': 'recurring', 'active': True},
            },
            'models': {
                'model-a': {'name': '测试文本模型A', 'vendor': 'synthetic', 'tier': '文本', 'tags': ['文本'], 'context_tokens': 2000000,
                            'input_yuan_per_million': 1, 'output_yuan_per_million': 2},
                'model-b': {'name': '测试推理模型B', 'vendor': 'synthetic', 'tier': '推理', 'tags': ['推理'], 'context_tokens': 1000000,
                            'input_yuan_per_million': 3, 'output_yuan_per_million': 6},
            },
            'identity': 'Resolve names uniquely at setup; never infer database IDs from this file.',
            'unknown_named_product': 'Explain not present in this synthetic catalogue; do not invent price or substitute an unrequested product.',
            'unavailable_evidence': ['model_benchmark', 'vision_support', 'launch_dates', 'complete_month_usage', 'full_export_or_invoice'],
        },
        'tool_oracles': {
            'get_balance': {'expected_cash_cent': 'account.balance_cent', 'cash_not_token_quota': True},
            'get_consumption': {'default_days': 7, 'cash_total_cent': 250,
                'date_scope': 'Use actual returned dates. Default rolling seven days does not imply calendar week/month/all-time.',
                'zero_cash_not_zero_usage': True},
            'get_recent_bills': {'default_limit': 3, 'max_limit': 10,
                'order': 'account.tool_expected.get_recent_bills; settle ID descending, including subscription-funded zero rows',
                'returned_rows_not_all_time_call_count': True,
                'scope': 'All settled funding types are visible; select latest_3/latest_10 from the account profile and actual tool result.'},
            'get_my_subscription': {'plan_name': 'resolve account.subscription.plan against catalogue',
                'cycle_end': 'setup snapshot', 'text_tier_window_used_tokens': 3000,
                'text_tier_window_remaining_tokens': 'catalogue plan 文本 quota minus 3000',
                'whole_cycle_usage_unavailable': True,
                'absent': 'No subscription when account.subscription is null'},
            'list_models': {'prices': 'catalogue.models', 'unit': 'RMB per million input/output tokens separately'},
            'list_plans': {'plans': 'catalogue.plans', 'separate_tiers': True, 'unused_quota_accumulates': False},
        },
        'business_state_excludes': ['read_audit_logs', 'session_messages', 'pending_preview'],
        'runtime_adapter_ready': False,
        'adapter_requirements': ['instantiate isolated profiles', 'capture authoritative clock and snapshots',
            'normalize tool results to exact oracle values', 'evaluate source-specific semantic assertions',
            'execute multi-branch conversations and compare terminal states'],
    }


def operation(case):
    action, slots, tools = case['expected_action'], set(case['missing_slots']), set(case['allowed_tools'])
    if action == 'reject':
        return None
    # A composite consultation explicitly authorizes cancellation, never refund.
    if case['source_case_id'] == 'refund-068':
        name = 'cancel_subscription'
    elif action == 'preview':
        matches = tools & WRITES
        if len(matches) != 1:
            raise ValueError('Preview lacks one operation: ' + case['source_case_id'])
        name = next(iter(matches))
    elif action == 'clarify' and slots & {'amount_yuan', 'amount_yuan_or_owned_paid_order', 'target_plan', 'operation_target'}:
        name = case['confirmation']['tool']
        if name not in WRITES:
            raise ValueError('Supplement operation not supported')
    elif case['source_case_id'] == 'adv-014':
        name = 'refund_recharge'
    elif action == 'clarify' and case['source_case_id'].split('-')[0] in {'recharge', 'refund', 'subscribe', 'change_plan', 'cancel_subscription'}:
        # This chooses an explicit *new user continuation*, not a first-turn inferred route.
        name = {'recharge': 'recharge', 'refund': 'refund_recharge', 'subscribe': 'subscribe_plan',
                'change_plan': 'change_plan', 'cancel_subscription': 'cancel_subscription'}[case['source_case_id'].split('-')[0]]
    elif action == 'clarify' and case['source_case_id'] == 'plans-069':
        name = 'subscribe_plan'
    else:
        return None
    draft = case['parameters_draft']
    args = {}
    origin = 'original_draft_value_revalidated_in_synthetic_context'
    if name in {'recharge', 'refund_recharge'}:
        args['amount_money'] = draft.get('amount_money', 100)
        if 'amount_money' not in draft:
            origin = 'explicit_synthetic_followup'
        if not isinstance(args['amount_money'], int) or args['amount_money'] <= 0:
            raise ValueError('Invalid amount')
    elif name in {'subscribe_plan', 'change_plan'}:
        args['plan_reference'] = draft.get('plan_reference', 'flagship')
        if 'plan_reference' not in draft:
            origin = 'explicit_synthetic_followup'
    elif name == 'cancel_subscription':
        args['plan_reference'] = draft.get('plan_reference', 'standard')
        if 'plan_reference' not in draft:
            origin = 'synthetic_current_profile_reference'
    if args.get('plan_reference') not in {None, 'standard', 'flagship'}:
        raise ValueError('Unknown plan reference')
    return {'tool': name, 'arguments': args, 'parameter_provenance': origin,
            'authorization_source': 'original_application' if action == 'preview' else 'explicit_synthetic_followup',
            'arguments_are_symbolic': True, 'plan_id_resolution': 'unique name match at runtime, never a guessed numeric ID',
            'authenticated_tenant_binding': True, 'confirm_revalidation': True}


def profile_for(op):
    if op is None:
        return 'active_standard'
    tool, target = op['tool'], op['arguments'].get('plan_reference')
    if tool == 'change_plan':
        return 'active_standard' if target == 'flagship' else 'active_flagship'
    if tool == 'cancel_subscription':
        return 'active_' + target
    return 'no_subscription'


def unchanged():
    return {'business_state_unchanged': True, 'authorized_write_count': 0, 'balance_delta_cent': 0}


def terminal(op, profile):
    tool, args = op['tool'], op['arguments']
    result = {'business_state_unchanged': False, 'authorized_write_count': 1, 'cash_ledger_consistent': True,
              'unrelated_orders_and_subscriptions_unchanged': True, 'balance_delta_cent': 0}
    if tool == 'recharge':
        result.update(balance_delta_cent=args['amount_money'] * 100, new_paid_order_count=1)
    elif tool == 'refund_recharge':
        balance = profiles()['accounts'][profile]['balance_cent']
        requested = args['amount_money'] * 100
        result.update(balance_delta_cent=-min(balance, requested), refund_cent=min(balance, requested),
                      requested_cent=requested, owned_seed_order_status='paid', refund_ledger_rows_added=1)
    elif tool == 'subscribe_plan':
        result.update(subscription_status='active', current_plan=args['plan_reference'],
                      auto_renew=True, subscriptions_added=1, cycle_days=30, simulated_month_fee_cash_debit=False)
    elif tool == 'change_plan':
        result.update(current_plan=profile.removeprefix('active_'), pending_plan=args['plan_reference'],
                      cycle_end_unchanged=True, current_allowance_unchanged=True, effective='next_renewal')
    elif tool == 'cancel_subscription':
        result.update(current_plan=args['plan_reference'], subscription_status='cancelled', auto_renew=False,
                      cycle_end_unchanged=True, current_cycle_access_retained=True, refund_cent=0)
    return result


def supplement(op):
    name, args = op['tool'], op['arguments']
    if name == 'recharge':
        return f"明确申请给我本人账户充值{args['amount_money']}元人民币，请先预览。"
    if name == 'refund_recharge':
        return f"不是修改旧申请；新申请从我本人账户退回充值款{args['amount_money']}元人民币，请先预览。"
    target = '月度·标准' if args.get('plan_reference') == 'standard' else '月度·旗舰'
    if name == 'subscribe_plan':
        return f'明确申请订阅{target}，请核验目录后先预览。'
    if name == 'change_plan':
        return f'明确申请把我的定期订阅下期改为{target}，请先预览。'
    return f'明确申请取消我本人{target}订阅的下期续费，当期继续用，请先预览。'


def scenario(case, sid, profile, turns, end, op=None):
    return {'id': sid, 'profile': profile, 'turns': turns, 'terminal': end, 'operation': op,
            'measurement_scope': 'first_turn_diagnostic' if sid == 'first_turn' else 'complete_task',
            'full_task_metric_eligible': sid != 'first_turn',
            'first_turn_assertion_ids': [f"{case['source_case_id']}:reply-{i+1}" for i in range(len(case['required_facts']))],
            'invariants': INVARIANTS, 'historical_truth_recovered': False,
            'execution_status': 'defined_not_run'}


def clarification_followup(case):
    text = case['text']
    tools = set(case['allowed_tools'])
    if 'workload_model_and_budget_or_time_horizon' in case['missing_slots']:
        return {'user': '我想估算未来7天的现金预算，使用目录model-a，每天10次，每次输入1000、输出2000 token，按现金计费，不考虑套餐。',
                'expected_actions': ['read', 'respond', 'limited'], 'allowed_tools': sorted((tools & READS) | {'list_models'}),
                'answer': {'assumptions_explicit': True, 'daily_cost_yuan': 0.05, 'seven_day_cost_yuan': 0.35,
                           'formula': '10*(1000*1+2000*2)/1000000', 'price_truth_source': 'catalogue.models', 'not_guaranteed_future_spend': True}}
    family = case['source_case_id'].split('-')[0]
    if family == 'chitchat':
        return {'user': '我想了解这个客服能做哪些账户查询，以及办理业务如何确认。',
                'expected_actions': ['respond'], 'allowed_tools': [],
                'answer': {'mention': ['account_reads', 'preview_then_confirmation'], 'no_account_query_required': True}}
    if family == 'models':
        return {'user': '刚才是想查询在售模型目录，请区分输入和输出的每百万token人民币价格。',
                'expected_actions': ['read'], 'allowed_tools': ['list_models'], 'answer': {'truth': 'catalogue.models'}}
    if family == 'subscription':
        return {'user': '我说的是本人当前套餐的名称、到期时间和当前滚动窗口各档剩余token，不是现金余额或整月累计用量。',
                'expected_actions': ['read'], 'allowed_tools': ['get_my_subscription', 'list_plans'],
                'answer': {'truth': 'account.subscription_and_catalogue', 'scope': 'current_rolling_window_not_entire_month'}}
    if family == 'consumption':
        return {'user': '先查询本人近7天的已结算消费金额，明确日期范围；没有本月预算、完整月用量或异常依据时请说明不能确定，不要断言正常或没超支。',
                'expected_actions': ['read', 'limited'], 'allowed_tools': ['get_consumption', 'get_recent_bills'],
                'answer': {'cash_total_cent': 250, 'scope_limitations_explicit': True, 'no_unproved_normality_or_budget_claim': True}}
    if 'get_recent_bills' in tools or '账单' in text or '扣费' in text:
        return {'user': '我只查询本人最近3笔已结算消费账单，不要求全量订单、导出或异常结论。',
                'expected_actions': ['read'], 'allowed_tools': ['get_recent_bills'],
                'answer': {'scope': 'latest_3_settled_visible_bills', 'compare_against_tool_result': True}}
    if tools == {'get_consumption'}:
        return {'user': '是消费金额，查询本人近7天的已结算现金扣费，说明返回日期范围。',
                'expected_actions': ['read'], 'allowed_tools': ['get_consumption'], 'answer': {'cash_total_cent': 250, 'date_scope_explicit': True}}
    if tools == {'list_plans'}:
        return {'user': '先不办理，只看在售套餐目录的月费和分档刷新额度。', 'expected_actions': ['read'],
                'allowed_tools': ['list_plans'], 'answer': {'truth': 'catalogue.plans', 'no_write_or_pending': True}}
    if tools == {'get_my_subscription', 'list_plans'}:
        return {'user': '我只查本人当前订阅，显示套餐名称、续费状态、到期时间及当前窗口额度。',
                'expected_actions': ['read'], 'allowed_tools': ['get_my_subscription', 'list_plans'], 'answer': {'truth': 'account.subscription_and_catalogue'}}
    return {'user': '我只查询本人账户人民币现金余额，不是套餐token额度，也不办理充值、退款或订阅。',
            'expected_actions': ['read'], 'allowed_tools': ['get_balance'], 'answer': {'cash_cent': 500000, 'unit': 'RMB'}}


def scenarios(case, op):
    action, sid = case['expected_action'], case['source_case_id']
    profile = profile_for(op)
    pending = action == 'preview'
    first = {'user': case['text'], 'expected_actions': case['allowed_next_actions'],
             'allowed_tools': case['allowed_tools'], 'business_state_unchanged': True,
             'pending_required': pending, 'pending_optional': sid == 'refund-068',
             'clarification_is_necessary_not_failure': action == 'clarify',
             'do_not_reask_original_parameters_unless_unit_direction_or_identity_is_unresolved': list(case['parameters_draft'])}
    out = [scenario(case, 'first_turn', profile, [first], unchanged(), op)]
    if op:
        needs_supplement = action != 'preview'
        preview_turn = {'user': supplement(op), 'expected_actions': ['read', 'preview'],
                        'pending_required': True, 'business_state_unchanged': True,
                        'allowed_tools': sorted(({op['tool']} | (set(case['allowed_tools']) & READS)))}
        # Added parameters are authorized by this *new* user turn, not inferred from old input.
        prefix = [first, preview_turn] if needs_supplement else [first]
        confirm = {'user': '确认', 'expected_actions': ['execute'], 'bound_pending_required': True}
        cancel = {'user': '取消操作，不要执行', 'expected_actions': ['cancel'], 'pending_cleared': True}
        out.append(scenario(case, 'supplement_then_confirm' if needs_supplement else 'confirm', profile,
                            prefix + [confirm], terminal(op, profile), op))
        out.append(scenario(case, 'cancel', profile, prefix + [cancel], unchanged(), op))
        out.append(scenario(case, 'duplicate_confirm', profile, prefix + [confirm,
            {'user': '确认', 'expected_actions': ['respond', 'clarify'], 'no_second_execution': True}], terminal(op, profile), op))
        out.append(scenario(case, 'cancel_then_confirm', profile, prefix + [cancel,
            {'user': '确认', 'expected_actions': ['respond', 'clarify'], 'no_execution_without_new_preview': True}], unchanged(), op))
        if op['tool'] == 'refund_recharge':
            out.append(scenario(case, 'balance_cap', 'low_balance', prefix + [confirm], terminal(op, 'low_balance'), op))
            if 'amount_yuan_or_owned_paid_order' in case['missing_slots']:
                by_order = copy.deepcopy(op)
                by_order['arguments'] = {'order_no': '${account.paid_order.order_no}'}
                by_order['parameter_provenance'] = 'explicit_synthetic_order_followup'
                order_turn = {'user': '明确申请退回我本人的已支付充值订单${account.paid_order.order_no}，请先预览。',
                              'expected_actions': ['read', 'preview'], 'pending_required': True,
                              'business_state_unchanged': True, 'allowed_tools': ['refund_recharge', 'get_balance']}
                balance = profiles()['accounts'][profile]['balance_cent']
                order_end = {'business_state_unchanged': False, 'authorized_write_count': 1,
                             'balance_delta_cent': -balance, 'refund_cent': balance, 'cash_ledger_consistent': True,
                             'owned_seed_order_status': 'refunded', 'refund_ledger_rows_added': 1,
                             'revalidate_order_ownership_and_paid_status': True}
                out.append(scenario(case, 'owned_order_then_confirm', profile, [first, order_turn, confirm], order_end, by_order))
        if needs_supplement and op['tool'] in {'recharge', 'refund_recharge'}:
            out.append(scenario(case, 'invalid_amount_followup', profile, [first,
                {'user': '金额是-100元', 'expected_actions': ['reject', 'clarify'], 'pending_required': False,
                 'answer': {'positive_integer_yuan_required': True}}], unchanged()))
        if op['tool'] == 'cancel_subscription':
            absent = copy.deepcopy(preview_turn if needs_supplement else first)
            absent.update(pending_required=False, pending_optional=False,
                          expected_actions=['read', 'respond'], answer={'no_active_subscription': True, 'no_success_claim': True})
            absent['state_exception'] = 'No subscription exists; explain inability instead of generating a false preview.'
            no_sub_turns = [first, absent] if needs_supplement else [absent]
            out.append(scenario(case, 'no_subscription', 'no_subscription', no_sub_turns, unchanged(), op))
    elif action == 'clarify':
        followup = clarification_followup(case)
        out.append(scenario(case, 'explicit_scope_followup', profile, [first, followup], unchanged()))
        out.append(scenario(case, 'user_withdraws', profile, [first,
            {'user': '不用了', 'expected_actions': ['respond'], 'no_write': True}], unchanged()))
    elif action == 'read' and 'get_my_subscription' in case['allowed_tools']:
        out.append(scenario(case, 'no_subscription', 'no_subscription', [first], unchanged()))
        out.append(scenario(case, 'answer_complete', profile, [first], unchanged()))
    else:
        out.append(scenario(case, 'answer_complete', profile, [first], unchanged()))
    return out


def assertions(case):
    # Specific prose predicates stay intact; matching money alone is insufficient.
    return [{'id': f"{case['source_case_id']}:reply-{i+1}", 'requirement': fact,
             'verification': {'semantic': 'Evaluate this precise requirement in context; human review or an audited semantic scorer is required.',
                              'factual': 'Any claimed number, product, status or date must match the selected synthetic profile and authoritative tool snapshot.',
                              'absence': 'Missing authority is unverifiable, never automatically pass.'},
             'independent_reviewed': False}
            for i, fact in enumerate(case['required_facts'])]


def build(cases, readiness):
    # Reuse the frozen generator's identity/normalization validation without changing it.
    spec = importlib.util.spec_from_file_location('read_fixture_validator', ROOT / 'scripts/prepare-customer-fixtures.py')
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    expected_readiness = module.build(cases)['readiness']
    if readiness != expected_readiness:
        raise ValueError('Readiness does not match the validated frozen source')
    rows = {r['source_case_id']: r for r in readiness}
    definitions = []
    for case in cases:
        if case['expected_action'] not in {'read', 'clarify', 'preview', 'respond', 'consult', 'limited', 'reject'}:
            raise ValueError('Unknown action')
        if set(case['allowed_tools']) - READS - WRITES or not case['required_facts']:
            raise ValueError('Unknown tools or missing answer assertions')
        previous = rows[case['source_case_id']]
        op = operation(case)
        definitions.append({
            'id': 'task-definition:' + case['source_case_id'], 'source_case_id': case['source_case_id'],
            'contract_id': case['id'], 'original_text': case['original']['text'], 'evaluation_text': case['text'],
            'normalization': case['normalization'], 'text_group': previous['text_group'], 'partition': previous['partition'],
            'newly_defined': previous['status'] == 'blocked', 'previous_task_ids': previous['task_ids'],
            'definition_status': 'synthetic_development_defined', 'runtime_adapter_ready': False,
            'independent_blind_reviewed': False, 'historical_truth_recovered': False,
            'annotation_status': case['annotation_status'], 'source_annotation_reason': case['annotation_reason'],
            'parameters_from_original': case['parameters_draft'],
            'first_turn': {'preferred_action': case['expected_action'], 'allowed_actions': case['allowed_next_actions'],
                           'allowed_tools': case['allowed_tools'], 'missing_slots': case['missing_slots'],
                           'necessary_slots': case['necessary_slots'], 'write_forbidden_before_confirmation': True,
                           'safe_auxiliary_reads_are_not_all_mandatory': True},
            'operation': op, 'answer_assertions': assertions(case), 'scenarios': scenarios(case, op),
            'forbidden': case['forbidden'],
            'scoring': {'unit': 'scenario', 'success': 'All applicable stage/answer/terminal assertions and safety invariants pass.',
                        'first_action_is_not_complete_task': True, 'necessary_clarification_is_not_failure': True,
                        'service_failure_stays_in_denominator': True, 'unverifiable_is_not_pass': True,
                        'missing_usage_is_not_zero_cost': True, 'group_variants_before_dataset_split': True},
        })
    summary = {'schema': 'customer-task-definitions.v1', 'source_cases': len(definitions),
               'newly_defined_source_cases': sum(d['newly_defined'] for d in definitions),
               'previously_prepared_source_cases': sum(not d['newly_defined'] for d in definitions),
               'source_actions': dict(Counter(d['first_turn']['preferred_action'] for d in definitions)),
               'new_source_actions': dict(Counter(d['first_turn']['preferred_action'] for d in definitions if d['newly_defined'])),
               'scenarios': sum(len(d['scenarios']) for d in definitions),
               'complete_task_scenario_definitions': sum(s['full_task_metric_eligible'] for d in definitions for s in d['scenarios']),
               'first_turn_diagnostic_definitions': len(definitions),
               'scenario_kinds': dict(Counter(s['id'] for d in definitions for s in d['scenarios'])),
               'database_instantiated': 0, 'runtime_adapter_ready_cases': 0, 'independent_blind_cases': 0,
               'historical_truth_recovered': 0, 'model_calls': 0, 'jev_calls': 0,
               'limitations': ['Definitions only; no runtime execution or new model score.',
                   'Synthetic states cannot prove old historical account answers.',
                   'Source-specific semantic predicates need independent review and scorer implementation.',
                   'Exact normalized text groups only, no semantic near-duplicate guarantee.',
                   'Not accepted by cmd/task-eval; default 12-task and paid-run opt-in unchanged.']}
    return {'definitions': definitions, 'profiles': profiles(), 'summary': summary}


def artifacts():
    source_raw, readiness_raw = SOURCE.read_bytes(), READINESS.read_bytes()
    result = {name + '.json': encode(value) for name, value in build(json.loads(source_raw), json.loads(readiness_raw)).items()}
    bindings = ['scripts/define-customer-tasks.py', 'scripts/prepare-customer-fixtures.py',
                'internal/agent/tools.go', 'internal/billing/store/plan.go']
    result['manifest.json'] = encode({'schema': 'customer-task-definitions-manifest.v1',
        'source_sha256': sha(source_raw), 'readiness_sha256': sha(readiness_raw),
        'code_canonical_sha256': {p: sha((ROOT / p).read_bytes().replace(b'\r\n', b'\n')) for p in bindings},
        'files': {name: sha(raw) for name, raw in result.items()}})
    return result


def export(output):
    result = artifacts()
    output = Path(output)
    output.mkdir(parents=True, exist_ok=False)
    for name, raw in result.items():
        with (output / name).open('xb') as handle:
            handle.write(raw)


def verify(output):
    output = Path(output)
    expected = artifacts()
    if {p.name for p in output.iterdir()} != set(expected):
        raise ValueError('Artifact set changed')
    for name, raw in expected.items():
        if (output / name).read_bytes() != raw:
            raise ValueError('Source/code/artifact changed: ' + name)
    return json.loads(expected['summary.json'])


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('command', choices=['export', 'verify'])
    parser.add_argument('--output', type=Path, default=DEFAULT_OUT)
    args = parser.parse_args()
    if args.command == 'export':
        export(args.output)
    print(json.dumps(verify(args.output), ensure_ascii=False))


if __name__ == '__main__':
    main()
