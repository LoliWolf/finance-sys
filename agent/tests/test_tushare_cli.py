from skills.tushare.scripts import tushare_call


def test_tushare_cli_accepts_token_argument(monkeypatch):
    observed = {}
    monkeypatch.setattr(
        tushare_call,
        "load_catalog",
        lambda: [
            {
                "api": "stock_basic",
                "title": "stock",
                "category": "basic",
                "description": "",
                "url": "https://example.invalid",
            }
        ],
    )

    def fake_call(args, params):
        observed["token"] = args.token
        observed["params"] = params
        return object()

    monkeypatch.setattr(tushare_call, "call_tushare", fake_call)
    monkeypatch.setattr(tushare_call, "select_fields_if_needed", lambda result, fields: result)
    monkeypatch.setattr(tushare_call, "write_output", lambda result, args: None)
    assert tushare_call.main(["stock_basic", "--token", "argv-token"]) == 0
    assert observed == {"token": "argv-token", "params": {}}
