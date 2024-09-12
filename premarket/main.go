package main

import (
	"context"
	"github.com/google/btree"
	"github.com/google/uuid"
	"github.com/hirokisan/bybit/v2"
	"github.com/shopspring/decimal"
)

func main() {
	err := start()
	if err != nil {
		return
	}
}

func start() error {

	wsClient := bybit.NewWebsocketClient()

	placeClient := bybit.NewClient().WithAuth("sJlex3P1lpJNA7he58", "nckirJCWUbGudUwcWLGHzCcgMMsI7Zo5Hbii")

	orderBookMap := OrderBookMapInit()

	orderManagerChan := make(chan *bybit.V5WebsocketPrivateOrderData, 1024)
	mktChan := make(chan *OrderBookDepthPara, 1024)

	executors := []bybit.WebsocketExecutor{}

	svcRoot := wsClient.V5()

	//共有订阅行情
	{
		svc, err := svcRoot.Public("linear")
		if err != nil {
			return err
		}
		//_, err = svc.SubscribeTrade(bybit.V5WebsocketPublicTradeParamKey{Symbol: bybit.SymbolV5CATIUSDT},
		//	func(response bybit.V5WebsocketPublicTradeResponse) error {
		//
		//		data, _ := json.Marshal(response.Data)
		//		println(string(data))
		//		return nil
		//	})
		//if err != nil {
		//	return err
		//}

		_, err = svc.SubscribeOrderBook(bybit.V5WebsocketPublicOrderBookParamKey{Depth: 50,
			Symbol: bybit.SymbolV5CATIUSDT},
			func(response bybit.V5WebsocketPublicOrderBookResponse) error {

				if response.Type == "snapshot" {
					// 重置 ob
					orderBookMap = OrderBookMapInit()
					for _, v := range response.Data.Asks {
						price, _ := decimal.NewFromString(v.Price)
						size, _ := decimal.NewFromString(v.Size)
						askOrderBook := orderBookMap.OM["a"]
						askOrderBook.ReplaceOrInsert(&OrderBook{
							Price: price,
							Size:  size,
							side:  bybit.SideSell,
						})
					}
					for _, v := range response.Data.Bids {
						price, _ := decimal.NewFromString(v.Price)
						size, _ := decimal.NewFromString(v.Size)
						askOrderBook := orderBookMap.OM["b"]
						askOrderBook.ReplaceOrInsert(&OrderBook{
							Price: price,
							Size:  size,
							side:  bybit.SideBuy,
						})

					}
				} else {
					for _, v := range response.Data.Asks {
						price, _ := decimal.NewFromString(v.Price)
						size, _ := decimal.NewFromString(v.Size)
						askOrderBook := orderBookMap.OM["a"]
						if size.IsZero() {
							askOrderBook.Delete(&OrderBook{
								Price: price,
								Size:  decimal.Decimal{},
								side:  bybit.SideSell,
							})
						} else {
							askOrderBook.ReplaceOrInsert(&OrderBook{
								Price: price,
								Size:  size,
								side:  bybit.SideSell,
							})
						}
					}
					for _, v := range response.Data.Bids {
						price, _ := decimal.NewFromString(v.Price)
						size, _ := decimal.NewFromString(v.Size)
						askOrderBook := orderBookMap.OM["b"]
						if size.IsZero() {
							askOrderBook.Delete(&OrderBook{
								Price: price,
								Size:  decimal.Decimal{},
								side:  bybit.SideBuy,
							})
						} else {
							askOrderBook.ReplaceOrInsert(&OrderBook{
								Price: price,
								Size:  size,
								side:  bybit.SideBuy,
							})
						}
					}
				}

				//println(fmt.Sprintf("ask1 price %s", orderBookMap.OM["a"].Max().(*OrderBook).Price.String()))
				//println(fmt.Sprintf("bid1 price %s", orderBookMap.OM["b"].Max().(*OrderBook).Price.String()))
				//data, _ := json.Marshal(response.Data)
				//println(fmt.Sprintf("type: %s data:%s", response.Type, string(data)))
				//第一 下单
				//高级点 做taker ob 深度 + 试算 手续费扣掉

				dp := &OrderBookDepthPara{
					Ask1Price: orderBookMap.OM["a"].Max().(*OrderBook).Price,
					Bid1Price: orderBookMap.OM["b"].Max().(*OrderBook).Price,
					Depth:     1,
				}
				mktChan <- dp
				return nil
			})
		if err != nil {
			return err
		}

		executors = append(executors, svc)
	}

	// 私有订阅

	{

		wsPrivateClient := bybit.NewWebsocketClient().WithAuth("sJlex3P1lpJNA7he58", "nckirJCWUbGudUwcWLGHzCcgMMsI7Zo5Hbii")
		svc, err := wsPrivateClient.V5().Private()
		if err != nil {
			// handle dialing error
		}

		err = svc.Subscribe()
		if err != nil {
			// handle subscription error
		}

		_, err = svc.SubscribePosition(func(position bybit.V5WebsocketPrivatePositionResponse) error {

			// handle new position information
			return nil
		})

		if err != nil {
			// handle registration error
		}

		_, err = svc.SubscribeOrder(func(response bybit.V5WebsocketPrivateOrderResponse) error {

			for _, v := range response.Data {
				orderManagerChan <- &v
			}
			return nil
		})

		if err != nil {
			return err
		}

		errHandler := func(isWebsocketClosed bool, err error) {
			//panic("connect ")
			// Connection issue (timeout, etc.).

			// At this point, the connection is dead and you must handle the reconnection yourself
		}

		go svc.Start(context.Background(), errHandler)
	}

	go wsClient.Start(context.Background(), executors)

	for {
		select {
		case o := <-orderManagerChan:
			println(o.OrderID)
		case mkt := <-mktChan:

			println(mkt.Bid1Price.String())
			println(mkt.Ask1Price.String())

			sym := bybit.SymbolV5CATIUSDT
			co := bybit.CoinUSDT
			res, err := placeClient.V5().Order().CancelAllOrders(bybit.V5CancelAllOrdersParam{
				Category:    bybit.CategoryV5Linear,
				Symbol:      &sym,
				BaseCoin:    nil,
				SettleCoin:  &co,
				OrderFilter: nil,
			})

			if err != nil {
				return err
			}

			println(res.RetCode)

			price := mkt.Bid1Price.Sub(decimal.New(5, -3)).String()
			//timeI := bybit.TimeInForceGoodTillCancel
			clientOrderId := uuid.New().String()
			qty := decimal.NewFromInt(20)
			resp2, errO := placeClient.V5().Order().CreateOrder(bybit.V5CreateOrderParam{
				Category:              bybit.CategoryV5Linear,
				Symbol:                sym,
				Side:                  bybit.SideBuy,
				OrderType:             bybit.OrderTypeLimit,
				Qty:                   qty.String(),
				IsLeverage:            nil,
				Price:                 &price,
				TriggerDirection:      nil,
				OrderFilter:           nil,
				TriggerPrice:          nil,
				TriggerBy:             nil,
				OrderIv:               nil,
				TimeInForce:           nil,
				PositionIdx:           nil,
				OrderLinkID:           &clientOrderId,
				TakeProfit:            nil,
				StopLoss:              nil,
				TpTriggerBy:           nil,
				SlTriggerBy:           nil,
				ReduceOnly:            nil,
				CloseOnTrigger:        nil,
				SmpType:               nil,
				MarketMakerProtection: nil,
				TpSlMode:              nil,
				TpLimitPrice:          nil,
				SlLimitPrice:          nil,
				TpOrderType:           nil,
				SlOrderType:           nil,
				MarketUnit:            nil,
			})
			if errO != nil {
				println(resp2.RetMsg)
			}
		}
	}

	return nil
}

type OrderBook struct {
	Price decimal.Decimal
	Size  decimal.Decimal
	side  bybit.Side
}

func (lf *OrderBook) Less(item btree.Item) bool {
	if v, ok := item.(*OrderBook); ok {

		if lf.side == bybit.SideBuy {
			return lf.Price.LessThan(v.Price)
		} else if lf.side == bybit.SideSell {
			return lf.Price.GreaterThan(v.Price)
		}
	}
	panic("error")
}

type OrderBookMap struct {
	OM map[string]*btree.BTree
}

func OrderBookMapInit() OrderBookMap {
	orderBookMap := OrderBookMap{OM: make(map[string]*btree.BTree)}
	orderBookMap.OM["a"] = btree.New(2)
	orderBookMap.OM["b"] = btree.New(2)
	return orderBookMap
}

type OrderBookDepthPara struct {
	Ask1Price decimal.Decimal
	Bid1Price decimal.Decimal
	Depth     int64
}

type Order struct {
	OrderId       string
	ClientOrderId string
}

type CoreEngine struct {
	*OrderBookDepthPara
}
