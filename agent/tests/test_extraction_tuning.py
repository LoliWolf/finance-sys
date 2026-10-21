import copy
import hashlib
import json
from pathlib import Path
from concurrent.futures import ThreadPoolExecutor

import httpx
import pytest
from pydantic import ValidationError

from app import config
from app.config import AgentSettings, LLMSettings
from app.llm_client import LLMClient, _parse_chat_completion, _NonRetryableLLMError
from app.nodes.classify import classify_untrackable_targets
from app.nodes.extract import extract_raw_intents
from app.schemas import AgentLimits, AgentRawIntent


def intent(i):
    return {"intent_id":f"intent-{i}","raw_symbol":f"公司{i}","direction":"LONG",
            "reference_price":0,"reference_price_note":"price_missing_in_text",
            "thesis":"明确关注","evidence":[{"chunk_index":0,"text":"明确关注"}],
            "risks":[],"confidence":0.8}


def completion(n=1):
    return {"choices":[{"message":{"content":json.dumps({"raw_intents":[intent(i) for i in range(n)]})}}]}


@pytest.mark.parametrize("temperature",[-0.1,1.1,float('nan'),float('inf')])
def test_temperature_rejects_invalid_values(temperature):
    with pytest.raises(ValidationError): LLMSettings(temperature=temperature)


@pytest.mark.parametrize("temperature",[i/10 for i in range(11)])
def test_all_sweep_temperatures_and_nacos_prompt_are_sent(temperature):
    def handler(request):
        body=json.loads(request.content)
        assert body['temperature']==temperature
        assert 'NACOS业务语义' in body['messages'][0]['content']
        assert 'Do not output entry_price' in body['messages'][0]['content']
        return httpx.Response(200,json=completion())
    settings=config._settings_from_nacos_config({'llm':{'enabled':True,'endpoint':'https://llm.test','model':'test','temperature':temperature,'extraction_prompt':'NACOS业务语义'}})
    client=LLMClient(settings,httpx.Client(transport=httpx.MockTransport(handler)))
    assert len(client.extract_document('原文',0).raw_intents)==1


def test_zero_limit_preserves_more_than_100_recommendations_and_validates_tail():
    assert len(_parse_chat_completion(completion(121),0).raw_intents)==121
    payload=completion(21)
    raw=json.loads(payload['choices'][0]['message']['content'])
    raw['raw_intents'][-1]['direction']='INVALID'
    payload['choices'][0]['message']['content']=json.dumps(raw)
    with pytest.raises(ValidationError): _parse_chat_completion(payload,0)


def test_finite_limit_fails_without_retry_or_partial_result():
    calls=[]
    def handler(request):
        calls.append(request)
        return httpx.Response(200,json=completion(21))
    client=LLMClient(AgentSettings(llm=LLMSettings(enabled=True,endpoint='https://llm.test',model='test',max_retries=2)),httpx.Client(transport=httpx.MockTransport(handler)))
    with pytest.raises(RuntimeError,match='intent limit exceeded'):
        client.extract_document('原文',20)
    assert len(calls)==1


def test_provider_truncation_never_returns_partial_intents():
    payload=completion()
    payload['choices'][0]['finish_reason']='length'
    with pytest.raises(_NonRetryableLLMError,match='truncated'):
        _parse_chat_completion(payload,0)


def test_limits_and_non_llm_nodes_do_not_silently_slice():
    assert AgentLimits().max_intents==0
    assert AgentLimits().max_untrackable_targets==0
    text=' '.join(f'{600000+i}.SH' for i in range(25))
    assert len(extract_raw_intents([(0,text)],0))==25
    with pytest.raises(ValueError,match='limit exceeded'): extract_raw_intents([(0,text)],20)
    intents=[AgentRawIntent(**dict(intent(i),raw_symbol=f'板块{i}')) for i in range(25)]
    assert len(classify_untrackable_targets(intents,0))==25
    with pytest.raises(ValueError,match='limit exceeded'): classify_untrackable_targets(intents,20)


def test_nacos_refresh_replaces_snapshot_and_preserves_verified_snapshot_on_failure(monkeypatch, caplog):
    monkeypatch.setenv('NACOS_SERVER_ADDR','nacos.test:8848')
    clock=[100.0]
    monkeypatch.setattr(config.time,'monotonic',lambda:clock[0])
    values={'llm':{'model':'test','temperature':0.1,'extraction_prompt':'first'}}
    loads=[]
    def load(_):
        loads.append(1)
        return copy.deepcopy(values)
    monkeypatch.setattr(config.NacosConfigLoader,'load',load)
    first=config.get_settings()
    values['llm'].update(temperature=0.8,extraction_prompt='second')
    assert config.get_settings() is first
    clock[0]+=config.SETTINGS_REFRESH_SECONDS
    second=config.get_settings()
    assert second.llm.temperature==0.8 and second.llm.extraction_prompt=='second'
    assert first.llm.temperature==0.1 and first.llm.extraction_prompt=='first'
    assert len(loads)==2
    def fail(_): raise RuntimeError('Nacos unavailable with sensitive-value-must-not-leak')
    monkeypatch.setattr(config.NacosConfigLoader,'load',fail)
    clock[0]+=config.SETTINGS_REFRESH_SECONDS
    assert config.get_settings() is second
    assert config.settings_snapshot_stale() is True
    assert 'sensitive-value-must-not-leak' not in caplog.text
    monkeypatch.setattr(config.NacosConfigLoader,'load',load)
    clock[0]+=config.SETTINGS_REFRESH_SECONDS
    assert config.get_settings().llm.temperature==0.8
    assert config.settings_snapshot_stale() is False


def test_nacos_first_load_is_serialized(monkeypatch):
    monkeypatch.setenv('NACOS_SERVER_ADDR','nacos.test:8848')
    calls=[]
    def load(_):
        calls.append(1)
        return {'llm':{'model':'test'}}
    monkeypatch.setattr(config.NacosConfigLoader,'load',load)
    with ThreadPoolExecutor(max_workers=4) as pool:
        values=list(pool.map(lambda _:config.get_settings(),range(8)))
    assert len(calls)==1
    assert all(v is values[0] for v in values)


def test_health_exposes_tuning_fingerprint_without_prompt_or_credentials(monkeypatch):
    from app import main
    settings=AgentSettings(llm=LLMSettings(temperature=0.7,extraction_prompt='private-business-prompt',api_key='private-key'))
    monkeypatch.setattr(main,'get_settings',lambda:settings)
    response=main.healthz()
    assert response['extraction_config']['temperature']==0.7
    assert response['extraction_config']['prompt_sha256']==hashlib.sha256(b'private-business-prompt').hexdigest()
    assert response['extraction_config']['unlimited_default'] is True
    assert 'private-business-prompt' not in json.dumps(response)
    assert 'private-key' not in json.dumps(response)


def test_example_nacos_prompt_and_temperature_stay_in_sync():
    root=Path(__file__).resolve().parents[2]
    example=json.loads((root/'configs/example_nacos_config.json').read_text())['llm']
    annotated=(root/'configs/example_nacos_config.annotated.jsonc').read_text()
    for key in ('temperature','extraction_prompt'):
        line=next(line.strip() for line in annotated.splitlines() if line.strip().startswith('"'+key+'":'))
        value=json.loads(line.split(':',1)[1].rstrip(','))
        assert value==example[key]
    assert example['extraction_prompt'].strip()


def test_agent_thread_capacity_uses_shared_processing_config():
    settings=config._settings_from_nacos_config({'processing':{'llm_max_concurrency':60}})
    assert settings.processing_max_concurrency==60
    with pytest.raises(ValidationError):
        config._settings_from_nacos_config({'processing':{'llm_max_concurrency':0}})
