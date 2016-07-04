/**
 * Copyright 2014 @ z3q.net.
 * name :
 * author : jarryliu
 * date : 2013-12-05 17:49
 * description :
 * history :
 */

package order

import (
	"errors"
	"fmt"
	"go2o/core/domain/interface/cart"
	"go2o/core/domain/interface/delivery"
	"go2o/core/domain/interface/enum"
	"go2o/core/domain/interface/member"
	"go2o/core/domain/interface/merchant"
	"go2o/core/domain/interface/merchant/shop"
	"go2o/core/domain/interface/order"
	"go2o/core/domain/interface/payment"
	"go2o/core/domain/interface/promotion"
	"go2o/core/domain/interface/sale"
	"go2o/core/domain/interface/sale/goods"
	"go2o/core/domain/interface/valueobject"
	"go2o/core/infrastructure/domain"
	"go2o/core/infrastructure/lbs"
	"go2o/core/infrastructure/log"
	"sync"
	"time"
)

var _ order.IOrderManager = new(orderManagerImpl)

type orderManagerImpl struct {
	_rep         order.IOrderRep
	_saleRep     sale.ISaleRep
	_cartRep     cart.ICartRep
	_goodsRep    goods.IGoodsRep
	_promRep     promotion.IPromotionRep
	_memberRep   member.IMemberRep
	_partnerRep  merchant.IMerchantRep
	_deliveryRep delivery.IDeliveryRep
	_valRep      valueobject.IValueRep
	_payRep      payment.IPaymentRep
	_merchant    merchant.IMerchant
}

func NewOrderManager(cartRep cart.ICartRep, partnerRep merchant.IMerchantRep,
	rep order.IOrderRep, payRep payment.IPaymentRep, saleRep sale.ISaleRep,
	goodsRep goods.IGoodsRep, promRep promotion.IPromotionRep,
	memberRep member.IMemberRep, deliveryRep delivery.IDeliveryRep,
	valRep valueobject.IValueRep) order.IOrderManager {

	return &orderManagerImpl{
		_rep:         rep,
		_cartRep:     cartRep,
		_saleRep:     saleRep,
		_goodsRep:    goodsRep,
		_promRep:     promRep,
		_memberRep:   memberRep,
		_payRep:      payRep,
		_partnerRep:  partnerRep,
		_deliveryRep: deliveryRep,
		_valRep:      valRep,
	}
}

func (this *orderManagerImpl) CreateOrder(val *order.ValueOrder,
	cart cart.ICart) order.IOrder {
	return newOrder(this, val, cart, this._partnerRep,
		this._rep, this._goodsRep, this._saleRep, this._promRep,
		this._memberRep, this._valRep)
}


// 生成空白订单,并保存返回对象
func (this *orderManagerImpl) CreateBlankOrder(*order.ValueOrder)order.IOrder{
	return nil
}

// 将购物车转换为订单
func (this *orderManagerImpl) ParseToOrder(c cart.ICart) (order.IOrder,
	member.IMember, error) {
	val := &order.ValueOrder{}
	var m member.IMember
	var err error

	if c == nil {
		return nil, m, cart.ErrEmptyShoppingCart
	}
	if err = c.Check(); err != nil {
		return nil, m, err
	}
	// 判断购买会员
	val.BuyerId = c.GetValue().BuyerId
	if val.BuyerId > 0 {
		m = this._memberRep.GetMember(val.BuyerId)
	}
	if m == nil {
		return nil, m, member.ErrSessionTimeout
	}

	val.VendorId = -1

	tf, of := c.GetFee()
	val.TotalFee = tf //总金额
	val.Fee = of      //实际金额
	val.PayFee = of
	val.DiscountFee = tf - of //优惠金额
	val.VendorId = -1
	val.Status = 1

	o := this.CreateOrder(val, c)
	return o, m, nil
}

func (this *orderManagerImpl) GetFreeOrderNo(vendorId int) string {
	return this._rep.GetFreeOrderNo(vendorId)
}

// 智能选择门店
func (this *orderManagerImpl) SmartChoiceShop(address string) (shop.IShop, error) {
	//todo: 应只选择线下实体店
	//todo: AggregateRootId
	dly := this._deliveryRep.GetDelivery(-1)

	lng, lat, err := lbs.GetLocation(address)
	if err != nil {
		return nil, errors.New("无法识别的地址：" + address)
	}
	var cov delivery.ICoverageArea = dly.GetNearestCoverage(lng, lat)
	if cov == nil {
		return nil, delivery.ErrNotCoveragedArea
	}
	shopId, _, err := dly.GetDeliveryInfo(cov.GetDomainId())
	return this._merchant.ShopManager().GetShop(shopId), err
}

// 生成支付单
func (this *orderManagerImpl) createPaymentOrder(m member.IMember,
	o order.IOrder) payment.IPaymentOrder {
	val := o.GetValue()
	v := &payment.PaymentOrderBean{
		BuyUser:     m.GetAggregateRootId(),
		PaymentUser: m.GetAggregateRootId(),
		VendorId:    0,
		OrderId:     0,
		// 支付单金额
		TotalFee: val.Fee,
		// 余额抵扣
		BalanceDiscount: 0,
		// 积分抵扣
		IntegralDiscount: 0,
		// 系统支付抵扣金额
		SystemDiscount: 0,
		// 优惠券金额
		CouponDiscount: 0,
		// 立减金额
		SubFee: 0,
		// 支付选项
		PaymentOpt: payment.OptPerm,
		// 支付方式
		PaymentSign: val.PaymentSign,
		//创建时间
		CreateTime: time.Now().Unix(),
		// 在线支付的交易单号
		OuterNo: "",
		//支付时间
		PaidTime: 0,
		// 状态:  0为未付款，1为已付款，2为已取消
		State: payment.StateNotYetPayment,
	}
	v.FinalFee = v.TotalFee - v.SubFee - v.SystemDiscount -
		v.IntegralDiscount - v.BalanceDiscount
	return this._payRep.CreatePaymentOrder(v)
}

// 应用优惠券
func (this *orderManagerImpl) applyCoupon(m member.IMember,
	py payment.IPaymentOrder, couponCode string) error {
	po := py.GetValue()
	cp := this._promRep.GetCouponByCode(
		m.GetAggregateRootId(), couponCode)
	// 如果优惠券不存在
	if cp == nil {
		return errors.New("优惠券无效")
	}
	// 获取优惠券
	coupon := cp.(promotion.ICouponPromotion)
	result, err := coupon.CanUse(m, po.TotalFee)
	if result {
		if coupon.CanTake() {
			_, err = coupon.GetTake(m.GetAggregateRootId())
			//如果未占用，则占用
			if err != nil {
				err = coupon.Take(m.GetAggregateRootId())
			}
		} else {
			_, err = coupon.GetBind(m.GetAggregateRootId())
		}
		if err != nil {
			domain.HandleError(err, "domain")
			err = errors.New("优惠券无效")
		} else {
			_, err = py.CouponDiscount(coupon) //应用优惠券
			// err = order.ApplyCoupon(coupon)
		}
	}
	return err
}

// 预生成订单及支付单
func (this *orderManagerImpl) PrepareOrder(c cart.ICart, subject string,
	couponCode string) (order.IOrder, payment.IPaymentOrder, error) {
	order, m, err := this.ParseToOrder(c)
	var py payment.IPaymentOrder
	if err == nil {
		py = this.createPaymentOrder(m, order)
		val := order.GetValue()
		if len(subject) > 0 {
			val.Subject = subject
			order.SetValue(val)
		}

		if len(couponCode) != 0 {
			err = this.applyCoupon(m, py, couponCode)
		}
	}
	return order, py, err
}

func (this *orderManagerImpl) SubmitOrder(c cart.ICart, subject string,
	couponCode string, useBalanceDiscount bool) (order.IOrder,
	payment.IPaymentOrder, error) {
	order, py, err := this.PrepareOrder(c, subject, couponCode)
	if err != nil {
		return order, py, err
	}
	orderNo, err := order.Submit()
	tradeNo := orderNo
	if err == nil {
		cv := c.GetValue()
		cv.PaymentOpt = enum.PaymentOnlinePay
		pyUpdate := false
		//todo: 设置配送门店
		//err = order.SetShop(cv.ShopId)
		//err = order.SetDeliver(cv.DeliverId)

		// 设置支付方式
		if err = py.SetPaymentSign(cv.PaymentOpt); err != nil {
			return order, py, err
		}

		// 处理支付单
		py.BindOrder(order.GetAggregateRootId(), tradeNo)
		if _, err = py.Save(); err != nil {
			err = errors.New("下单出错:" + err.Error())
			order.Cancel(err.Error())
			domain.HandleError(err, "domain")
			return order, py, err
		}

		// 使用余额支付
		if useBalanceDiscount {
			err = py.BalanceDiscount()
			pyUpdate = true
		}

		// 更新支付单
		if err == nil && pyUpdate {
			_, err = py.Save()
		}
	}
	return order, py, err
}

// 根据订单编号获取订单
func (this *orderManagerImpl) GetOrderById(orderId int) order.IOrder {
	val := this._rep.GetOrderById(orderId)
	if val != nil {
		val.Items = this._rep.GetOrderItems(val.Id)
		return this.CreateOrder(val, nil)
	}
	return nil
}

// 根据订单号获取订单
func (this *orderManagerImpl) GetOrderByNo(orderNo string) order.IOrder {
	val := this._rep.GetValueOrderByNo(orderNo)
	if val != nil {
		val.Items = this._rep.GetOrderItems(val.Id)
		return this.CreateOrder(val, nil)
	}
	return nil
}

// 在线交易支付
func (this *orderManagerImpl) PaymentForOnlineTrade(orderId int) error {
	o := this.GetOrderById(orderId)
	if o == nil {
		return order.ErrNoSuchOrder
	}
	return o.PaymentForOnlineTrade("", "")
}

var (
	shopLocker sync.Mutex
	biShops    []shop.IShop
)

// 自动设置订单
func (this *orderManagerImpl) OrderAutoSetup(f func(error)) {
	var orders []*order.ValueOrder
	var err error

	shopLocker.Lock()
	defer func() {
		shopLocker.Unlock()
	}()
	biShops = nil
	log.Println("[SETUP] start auto setup")

	saleConf := this._merchant.ConfManager().GetSaleConf()
	if saleConf.AutoSetupOrder == 1 {
		orders, err = this._rep.GetWaitingSetupOrders(-1)
		if err != nil {
			f(err)
			return
		}

		dt := time.Now()
		for _, v := range orders {
			this.setupOrder(v, &saleConf, dt, f)
		}
	}
}

const (
	order_timeout_hour   = 24
	order_confirm_minute = 4
	order_process_minute = 11
	order_sending_minute = 31
	order_receive_hour   = 5
	order_complete_hour  = 11
)

func (this *orderManagerImpl) SmartConfirmOrder(order order.IOrder) error {

	return nil

	//todo:  自动确认订单
	var err error
	v := order.GetValue()
	log.Printf("[ AUTO][OrderSetup]:%s - Confirm \n", v.OrderNo)
	var sp shop.IShop
	if biShops == nil {
		// /pay/return_alipay?out_trade_no=ZY1607375766&request_token=requestToken&result=success&trade_no
		// =2016070221001004880246862127&sign=75a18ca0d75750ac22fedbbe6468c187&sign_type=MD5
		//todo:  拆分订单
		biShops = this._merchant.ShopManager().GetBusinessInShops()
	}
	if len(biShops) == 1 {
		sp = biShops[0]
	} else {
		sp, err = this.SmartChoiceShop(v.DeliverAddress)
		if err != nil {
			order.Suspend("智能分配门店失败！原因：" + err.Error())
			return err
		}
	}

	if sp != nil && sp.Type() == shop.TypeOfflineShop {
		sv := sp.GetValue()
		order.SetShop(sp.GetDomainId())
		err = order.Confirm()
		//err = order.Process()
		ofs := sp.(shop.IOfflineShop).GetShopValue()
		order.AppendLog(enum.ORDER_LOG_SETUP, false, fmt.Sprintf(
			"自动分配门店:%s,电话：%s", sv.Name, ofs.Tel))
	}
	return err
}

func (this *orderManagerImpl) setupOrder(v *order.ValueOrder,
	conf *merchant.SaleConf, t time.Time, f func(error)) {
	var err error
	order := this.CreateOrder(v, nil)
	dur := time.Duration(t.Unix()-v.CreateTime) * time.Second

	switch v.Status {
	case enum.ORDER_WAIT_PAYMENT:
		if v.IsPaid == 0 && dur > time.Minute*time.Duration(conf.OrderTimeOutMinute) {
			order.Cancel("超时未付款，系统取消")
			log.Printf("[ AUTO][OrderSetup]:%s - Payment Timeout\n", v.OrderNo)
		}

	case enum.ORDER_WAIT_CONFIRM:
		if dur > time.Minute*time.Duration(conf.OrderConfirmAfterMinute) {
			err = this.SmartConfirmOrder(order)
		}

	//		case enum.ORDER_WAIT_DELIVERY:
	//			if dur > time.Minute*order_process_minute {
	//				err = order.Process()
	//				if ctx.Debug() {
	//					ctx.Log().Printf("[ AUTO][OrderSetup]:%s - Processing \n", v.OrderNo)
	//				}
	//			}

	//		case enum.ORDER_WAIT_RECEIVE:
	//			if dur > time.Hour * conf.OrderTimeOutReceiveHour {
	//				err = order.Deliver()
	//				if ctx.Debug() {
	//					ctx.Log().Printf("[ AUTO][OrderSetup]:%s - Sending \n", v.OrderNo)
	//				}
	//			}
	case enum.ORDER_WAIT_RECEIVE:
		if dur > time.Hour*time.Duration(conf.OrderTimeOutReceiveHour) {
			err = order.SignReceived()

			log.Printf("[ AUTO][OrderSetup]:%s - Received \n", v.OrderNo)
			if err == nil {
				err = order.Complete()
				log.Printf("[ AUTO][OrderSetup]:%s - Complete \n", v.OrderNo)
			}
		}

		//		case enum.ORDER_COMPLETED:
		//			if dur > time.Hour*order_complete_hour {
		//				err = order.Complete()
		//				if ctx.Debug() {
		//					ctx.Log().Printf("[ AUTO][OrderSetup]:%s - Complete \n", v.OrderNo)
		//				}
		//			}
	}

	if err != nil {
		f(err)
	}
}
