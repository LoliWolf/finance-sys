package instrument

import "testing"

func TestParseCanonicalSecuritySymbols(t *testing.T) {
	tests := []struct {
		raw, market, assetType string
		symbol, tsCode         string
		eastmoney, board       string
	}{
		{"600000.SH", "", "A_SHARE", "600000", "600000.SH", "SHSE.600000", BoardSHMain},
		{"000001.SZ", "", "STOCK", "000001", "000001.SZ", "SZSE.000001", BoardSZMain},
		{"300502.SZ", "SZ", "STOCK", "300502", "300502.SZ", "SZSE.300502", BoardChiNext},
		{"688002.SH", "SH", "STOCK", "688002", "688002.SH", "SHSE.688002", BoardSTAR},
		{"SHSE.510300", "", "ETF", "510300", "510300.SH", "SHSE.510300", BoardETF},
		{"159915", "SZ", "ETF", "159915", "159915.SZ", "SZSE.159915", BoardETF},
	}
	for _, item := range tests {
		t.Run(item.raw, func(t *testing.T) {
			got, err := Parse(item.raw, item.market, item.assetType)
			if err != nil {
				t.Fatal(err)
			}
			if got.Symbol != item.symbol || got.TSCode != item.tsCode || got.EastmoneySymbol != item.eastmoney || got.BoardType != item.board {
				t.Fatalf("unexpected canonical value: %+v", got)
			}
		})
	}
}

func TestParseRejectsMarketConflict(t *testing.T) {
	if _, err := Parse("300502.SZ", "SH", "STOCK"); err == nil {
		t.Fatal("expected market conflict")
	}
}

func TestTradingUnitRules(t *testing.T) {
	standard, err := UnitRule(BoardChiNext)
	if err != nil {
		t.Fatal(err)
	}
	if value, err := RoundBuyVolume(199, standard); err != nil || value != 100 {
		t.Fatalf("unexpected standard buy volume %d, err=%v", value, err)
	}
	star, err := UnitRule(BoardSTAR)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RoundBuyVolume(100, star); err == nil {
		t.Fatal("expected STAR 100-share buy to fail")
	}
	if value, err := RoundBuyVolume(201, star); err != nil || value != 201 {
		t.Fatalf("unexpected STAR buy volume %d, err=%v", value, err)
	}
	if value, err := RoundSellVolume(199, 199, star); err != nil || value != 199 {
		t.Fatalf("unexpected STAR residual sell volume %d, err=%v", value, err)
	}
}
