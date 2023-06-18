package database

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gwaylib/errors"
)

func TestMarketDeal(t *testing.T) {
	InitDB("./")
	info := &MarketDealInfo{
		ID:        uuid.New().String(),
		UpdatedAt: time.Now(),
		Sid:       uuid.New().String(),
	}
	if err := AddMarketDealInfo(info); err != nil {
		t.Fatal(err)
	}
	if _, err := GetMarketDealInfo(info.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := GetMarketDealInfoBySid(info.Sid); err != nil {
		t.Fatal(err)
	}
	if _, err := ListMarketDealInfo(info.UpdatedAt.AddDate(0, 0, -1), time.Now(), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := GetMarketDealInfo("noid"); !errors.ErrNoData.Equal(err) {
		t.Fatal(err)
	}
	if _, err := GetMarketDealInfo("nosid"); !errors.ErrNoData.Equal(err) {
		t.Fatal(err)
	}
	info.DealNum = 1
	info.Offset = 0
	if err := UpdateMarketDeal(info); err != nil {
		t.Fatal(err)
	}
	if err := SetMarketDealState(info.ID, 200); err != nil {
		t.Fatal(err)
	}
}
