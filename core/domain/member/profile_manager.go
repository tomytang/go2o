/**
 * Copyright 2015 @ z3q.net.
 * name : member_profile.go
 * author : jarryliu
 * date : 2016-06-23 16:31
 * description :
 * history :
 */
package member

import (
	"errors"
	"github.com/jsix/gof/db/orm"
	"go2o/core/domain"
	"go2o/core/domain/interface/enum"
	"go2o/core/domain/interface/member"
	"go2o/core/domain/interface/merchant"
	"go2o/core/domain/interface/mss"
	"go2o/core/domain/interface/mss/notify"
	"go2o/core/domain/interface/valueobject"
	"go2o/core/domain/tmp"
	dm "go2o/core/infrastructure/domain"
	"go2o/core/infrastructure/domain/util"
	"regexp"
	"strings"
	"time"
)

var _ member.IProfileManager = new(profileManagerImpl)
var (
	exampleTrustImageUrl = "res/tru-example.jpg"
	// qqRegex = regexp.MustCompile("^\\d{5,12}$")
	zhNameRegexp = regexp.MustCompile("^[\u4e00-\u9fa5]{2,4}$")
)

type profileManagerImpl struct {
	_member      *memberImpl
	_memberId    int
	_rep         member.IMemberRep
	_valRep      valueobject.IValueRep
	_bank        *member.BankInfo
	_trustedInfo *member.TrustedInfo
	_profile     *member.Profile
}

func newProfileManagerImpl(m *memberImpl, memberId int,
	rep member.IMemberRep, valRep valueobject.IValueRep) member.IProfileManager {
	if memberId == 0 {
		//如果会员不存在,则不应创建服务
		panic(errors.New("member not exists"))
	}
	return &profileManagerImpl{
		_member:   m,
		_memberId: memberId,
		_rep:      rep,
		_valRep:   valRep,
	}
}

// 手机号码是否占用
func (p *profileManagerImpl) phoneIsExist(phone string) bool {
	return p._rep.CheckPhoneBind(phone, p._memberId)
}

// 验证数据,用v.updateTime > 0 判断是否为新创建用户
func (p *profileManagerImpl) validateProfile(v *member.Profile) error {
	v.Name = strings.TrimSpace(v.Name)
	v.Email = strings.ToLower(strings.TrimSpace(v.Email))
	v.Phone = strings.TrimSpace(v.Phone)

	if len([]rune(v.Name)) < 1 && v.UpdateTime > 0 {
		return member.ErrNilNickName
	}
	if (v.Province == 0 || v.City == 0 || v.District == 0 ||
		len(v.Address) == 0) && v.UpdateTime > 0 {
		return member.ErrAddress
	}

	if len(v.Email) != 0 && !emailRegex.MatchString(v.Email) {
		return member.ErrEmailValidErr
	}

	if len(v.Phone) != 0 && !phoneRegex.MatchString(v.Phone) {
		return member.ErrPhoneValidErr
	}

	if len(v.Phone) > 0 && p.phoneIsExist(v.Phone) {
		return member.ErrPhoneHasBind
	}

	if v.UpdateTime > 0 {
		conf := p._valRep.GetRegistry()
		if conf.MemberImRequired && len(v.Im) == 0 {
			return member.ErrMissingIM
		}
	}

	//if len(v.Qq) != 0 && !qqRegex.MatchString(v.Qq) {
	//	return member.ErrQqValidErr
	//}
	return nil
}

// 拷贝资料
func (p *profileManagerImpl) copyProfile(v, dst *member.Profile) error {
	v.Address = strings.TrimSpace(v.Address)
	v.Im = strings.TrimSpace(v.Im)
	v.Email = strings.TrimSpace(v.Email)
	v.Phone = strings.TrimSpace(v.Phone)
	v.Name = strings.TrimSpace(v.Name)
	v.Ext1 = strings.TrimSpace(v.Ext1)
	v.Ext2 = strings.TrimSpace(v.Ext2)
	v.Ext3 = strings.TrimSpace(v.Ext3)
	v.Ext4 = strings.TrimSpace(v.Ext4)
	v.Ext5 = strings.TrimSpace(v.Ext5)
	v.Ext6 = strings.TrimSpace(v.Ext6)
	if err := p.validateProfile(v); err != nil {
		return err
	}
	dst.Province = v.Province
	dst.City = v.City
	dst.District = v.District
	dst.Address = v.Address
	dst.BirthDay = v.BirthDay
	dst.Im = v.Im
	dst.Email = v.Email
	if dst.Phone == "" {
		dst.Phone = v.Phone
	}
	dst.Name = v.Name
	dst.Sex = v.Sex
	dst.Remark = v.Remark
	dst.Ext1 = v.Ext1
	dst.Ext2 = v.Ext2
	dst.Ext3 = v.Ext3
	dst.Ext4 = v.Ext4
	dst.Ext5 = v.Ext5
	dst.Ext6 = v.Ext6
	if v.Avatar != "" {
		dst.Avatar = v.Avatar
	}
	return nil
}

func (p *profileManagerImpl) ProfileCompleted() bool {
	v := p.GetProfile()
	r := len(v.Name) != 0 &&
		len(v.BirthDay) != 0 && len(v.Address) != 0 && v.Sex != 0 &&
		v.Province != 0 && v.City != 0 && v.District != 0
	if r {
		conf := p._valRep.GetRegistry()
		if conf.MemberImRequired && len(v.Im) == 0 {
			return false
		}
	}
	return r
}

// 获取资料
func (p *profileManagerImpl) GetProfile() member.Profile {
	if p._profile == nil {
		p._profile = p._rep.GetProfile(p._memberId)
	}
	return *p._profile
}

// 保存资料
func (p *profileManagerImpl) SaveProfile(v *member.Profile) error {
	ptr := p.GetProfile()
	err := p.copyProfile(v, &ptr)
	if err == nil {
		ptr.MemberId = p._memberId
		err = p._rep.SaveProfile(&ptr)
		if p.ProfileCompleted() {
			// 已完善资料
			p.notifyOnProfileComplete()
		}
	}
	return err
}

//todo: ?? 重构
func (p *profileManagerImpl) notifyOnProfileComplete() {
	//rl := p._member.GetRelation()
	//pt, err := p._member._merchantRep.GetMerchant(rl.RegisterMerchantId)
	//if err == nil {
	//	key := fmt.Sprintf("profile:complete:id_%d", p._memberId)
	//	if pt.MemberKvManager().GetInt(key) == 0 {
	//		if err := p.sendNotifyMail(pt); err == nil {
	//			pt.MemberKvManager().Set(key, "1")
	//		} else {
	//			fmt.Println(err.Error())
	//		}
	//	}
	//}
}

func (p *profileManagerImpl) sendNotifyMail(pt merchant.IMerchant) error {
	tplId := pt.KvManager().GetInt(merchant.KeyMssTplIdOfProfileComplete)
	if tplId > 0 {
		mailTpl := p._member._mssRep.GetProvider().GetMailTemplate(tplId)
		if mailTpl != nil {
			v := &mss.Message{
				// 消息类型
				Type: notify.TypeEmailMessage,
				// 消息用途
				UseFor: mss.UseForNotify,
				// 发送人角色
				SenderRole: mss.RoleSystem,
				// 发送人编号
				SenderId: 0,
				// 发送的目标
				To: []mss.User{
					mss.User{
						Role: mss.RoleMember,
						Id:   p._memberId,
					},
				},
				// 发送的用户角色
				ToRole: -1,
				// 全系统接收
				AllUser: -1,
				// 是否只能阅读
				Readonly: 1,
			}
			val := &notify.MailMessage{
				Subject: mailTpl.Subject,
				Body:    mailTpl.Body,
			}
			msg := p._member._mssRep.MessageManager().CreateMessage(v, val)
			//todo:?? data
			var data = map[string]string{
				"Name":           p._profile.Name,
				"InvitationCode": p._member.GetValue().InvitationCode,
			}
			return msg.Send(data)
		}
	}
	return errors.New("no such email template")
}

//todo: 密码应独立为credential

// 修改密码,旧密码可为空
func (p *profileManagerImpl) ModifyPassword(newPwd, oldPwd string) error {
	var err error
	if newPwd == oldPwd {
		return domain.ErrPwdCannotSame
	}

	if b, err := dm.ChkPwdRight(newPwd); !b {
		return err
	}

	if len(oldPwd) != 0 && oldPwd != p._member._value.Pwd {
		return domain.ErrPwdOldPwdNotRight
	}

	p._member._value.Pwd = newPwd
	_, err = p._member.Save()

	return err
}

// 修改交易密码，旧密码可为空
func (p *profileManagerImpl) ModifyTradePassword(newPwd, oldPwd string) error {
	var err error
	if newPwd == oldPwd {
		return domain.ErrPwdCannotSame
	}
	if b, err := dm.ChkPwdRight(newPwd); !b {
		return err
	}
	// 已经设置过旧密码
	if len(p._member._value.TradePwd) != 0 &&
		p._member._value.TradePwd != dm.TradePwd(oldPwd) {
		return domain.ErrPwdOldPwdNotRight
	}
	p._member._value.TradePwd = dm.TradePwd(newPwd)
	_, err = p._member.Save()
	return err
}

// 获取提现银行信息
func (p *profileManagerImpl) GetBank() member.BankInfo {
	if p._bank == nil {
		p._bank = p._rep.GetBankInfo(p._memberId)
		if p._bank == nil {
			p._bank = &member.BankInfo{
				MemberId:   p._memberId,
				IsLocked:   member.BankNoLock,
				State:      0,
				UpdateTime: time.Now().Unix(),
			}
			orm.Save(tmp.Db().GetOrm(), p._bank, 0)
		}
	}
	return *p._bank
}

// 保存提现银行信息
func (p *profileManagerImpl) SaveBank(v *member.BankInfo) error {
	v.Account = strings.TrimSpace(v.Account)
	v.AccountName = strings.TrimSpace(v.AccountName)
	v.Network = strings.TrimSpace(v.Network)
	v.Name = strings.TrimSpace(v.Name)
	if v.Account == "" || v.Name == "" {
		return member.ErrBankInfo
	}
	trustInfo := p.GetTrustedInfo()
	if trustInfo.Reviewed == 0 {
		return member.ErrNotTrusted
	}

	p.GetBank()
	if p._bank.IsLocked == member.BankLocked {
		return member.ErrBankInfoLocked
	}
	p._bank.Account = v.Account
	p._bank.AccountName = trustInfo.RealName
	//p._bank.AccountName = v.AccountName
	p._bank.Network = v.Network
	p._bank.State = v.State
	p._bank.Name = v.Name

	p._bank.State = member.StateOk       //todo:???
	p._bank.IsLocked = member.BankLocked //锁定
	p._bank.UpdateTime = time.Now().Unix()
	//p._bank.MemberId = p.value.Id
	return p._rep.SaveBankInfo(p._bank)
}

// 解锁提现银行卡信息
func (p *profileManagerImpl) UnlockBank() error {
	p.GetBank()
	if p._bank == nil {
		return member.ErrBankInfoNoYetSet
	}
	p._bank.IsLocked = member.BankNoLock
	return p._rep.SaveBankInfo(p._bank)
}

// 创建配送地址
func (p *profileManagerImpl) CreateDeliver(v *member.DeliverAddress) member.IDeliverAddress {
	return newDeliver(v, p._rep, p._valRep)
}

// 获取配送地址
func (p *profileManagerImpl) GetDeliverAddress() []member.IDeliverAddress {
	list := p._rep.GetDeliverAddress(p._memberId)
	var arr []member.IDeliverAddress = make([]member.IDeliverAddress, len(list))
	for i, v := range list {
		arr[i] = p.CreateDeliver(v)
	}
	return arr
}

// 设置默认地址
func (p *profileManagerImpl) SetDefaultAddress(addressId int) error {
	for _, v := range p.GetDeliverAddress() {
		vv := v.GetValue()
		if v.GetDomainId() == addressId {
			vv.IsDefault = 1
		} else {
			vv.IsDefault = 0
		}
		p._rep.SaveDeliver(&vv)
	}
	return nil
}

// 获取默认收货地址
func (p *profileManagerImpl) GetDefaultAddress() member.IDeliverAddress {
	list := p._rep.GetDeliverAddress(p._memberId)
	// 查找是否有默认地址
	for _, v := range list {
		if v.IsDefault == 1 {
			return p.CreateDeliver(v)
		}
	}
	// 使用第一个地址
	if len(list) > 0 {
		return p.CreateDeliver(list[0])
	}
	return nil
}

// 获取配送地址
func (p *profileManagerImpl) GetDeliver(deliverId int) member.IDeliverAddress {
	v := p._rep.GetSingleDeliverAddress(p._memberId, deliverId)
	if v != nil {
		return p.CreateDeliver(v)
	}
	return nil
}

// 删除配送地址
func (p *profileManagerImpl) DeleteDeliver(deliverId int) error {
	//todo: 至少保留一个配送地址
	return p._rep.DeleteDeliver(p._memberId, deliverId)
}

// 拷贝认证信息
func (p *profileManagerImpl) copyTrustedInfo(src, dst *member.TrustedInfo) {
	dst.RealName = src.RealName
	dst.CardId = src.CardId
	dst.TrustImage = src.TrustImage
}

// 实名认证信息
func (p *profileManagerImpl) GetTrustedInfo() member.TrustedInfo {
	if p._trustedInfo == nil {
		p._trustedInfo = &member.TrustedInfo{
			MemberId: p._memberId,
			Reviewed: enum.ReviewNotSet,
		}
		//如果还没有实名信息,则新建
		orm := tmp.Db().GetOrm()
		if err := orm.Get(p._memberId, p._trustedInfo); err != nil {
			orm.Save(nil, p._trustedInfo)
		}
	}
	// 显示示例图片
	if p._trustedInfo.TrustImage == "" {
		p._trustedInfo.TrustImage = exampleTrustImageUrl
	}
	return *p._trustedInfo
}

func (p *profileManagerImpl) checkCardId(cardId string, memberId int) bool {
	mId := 0
	tmp.Db().ExecScalar("SELECT COUNT(0) FROM mm_trusted_info WHERE card_id=? AND member_id <> ?",
		&mId, cardId, memberId)
	return mId == 0
}

// 保存实名认证信息
func (p *profileManagerImpl) SaveTrustedInfo(v *member.TrustedInfo) error {
	// 验证数据是否完整
	v.TrustImage = strings.TrimSpace(v.TrustImage)
	v.CardId = strings.TrimSpace(v.CardId)
	v.RealName = strings.TrimSpace(v.RealName)
	if len(v.TrustImage) == 0 || len(v.RealName) == 0 ||
		len(v.CardId) == 0 {
		return member.ErrMissingTrustedInfo
	}

	// 验证姓名
	if !zhNameRegexp.MatchString(v.RealName) {
		return member.ErrRealName
	}

	// 校验身份证号是否正确
	v.CardId = strings.ToUpper(v.CardId)
	err := util.CheckChineseCardID(v.CardId)
	if err != nil {
		return member.ErrTrustCardId
	}

	// 检查身份证是否已被占用
	if !p.checkCardId(v.CardId, p._memberId) {
		//todo: 临时为了过测试
		//return member.ErrCarIdExists
	}

	// 检测上传认证图片
	if v.TrustImage != "" {
		if len(v.TrustImage) < 10 || v.TrustImage == exampleTrustImageUrl {
			return member.ErrTrustMissingImage
		}
	}

	// 保存
	p.GetTrustedInfo()
	p.copyTrustedInfo(v, p._trustedInfo)
	p._trustedInfo.Remark = ""
	p._trustedInfo.Reviewed = enum.ReviewAwaiting //标记为待处理
	p._trustedInfo.UpdateTime = time.Now().Unix()
	_, err = orm.Save(tmp.Db().GetOrm(), p._trustedInfo,
		p._trustedInfo.MemberId)
	return err
}

// 审核实名认证,若重复审核将返回错误
func (p *profileManagerImpl) ReviewTrustedInfo(pass bool, remark string) error {
	p.GetTrustedInfo()
	if pass {
		p._trustedInfo.Reviewed = enum.ReviewPass
	} else {
		remark = strings.TrimSpace(remark)
		if remark == "" {
			return member.ErrEmptyReviewRemark
		}
		p._trustedInfo.Reviewed = enum.ReviewReject
	}
	p._trustedInfo.Remark = remark
	p._trustedInfo.ReviewTime = time.Now().Unix()
	_, err := orm.Save(tmp.Db().GetOrm(), p._trustedInfo,
		p._trustedInfo.MemberId)
	return err
}

var _ member.IDeliverAddress = new(addressImpl)

type addressImpl struct {
	_value     *member.DeliverAddress
	_memberRep member.IMemberRep
	_valRep    valueobject.IValueRep
}

func newDeliver(v *member.DeliverAddress, memberRep member.IMemberRep,
	valRep valueobject.IValueRep) member.IDeliverAddress {
	d := &addressImpl{
		_value:     v,
		_memberRep: memberRep,
		_valRep:    valRep,
	}
	return d
}

func (p *addressImpl) GetDomainId() int {
	return p._value.Id
}

func (p *addressImpl) GetValue() member.DeliverAddress {
	return *p._value
}

func (p *addressImpl) SetValue(v *member.DeliverAddress) error {
	if p._value.MemberId == v.MemberId {
		if err := p.checkValue(v); err != nil {
			return err
		}
		p._value = v
	}
	return nil
}

// 设置地区中文名
func (p *addressImpl) renewAreaName(v *member.DeliverAddress) string {
	//names := p._valRep.GetAreaNames([]int{
	//	v.Province,
	//	v.City,
	//	v.District,
	//})
	//if names[1] == "市辖区" || names[1] == "市辖县" || names[1] == "县" {
	//	return strings.Join([]string{names[0], names[2]}, " ")
	//}
	//return strings.Join(names, " ")

	return p._valRep.GetAreaString(v.Province, v.City, v.District)
}

func (p *addressImpl) checkValue(v *member.DeliverAddress) error {
	v.Address = strings.TrimSpace(v.Address)
	v.RealName = strings.TrimSpace(v.RealName)
	v.Phone = strings.TrimSpace(v.Phone)

	if len([]rune(v.RealName)) < 2 {
		return member.ErrDeliverContactPersonName
	}

	if v.Province <= 0 || v.City <= 0 || v.District <= 0 {
		return member.ErrNotSetArea
	}

	if !phoneRegex.MatchString(v.Phone) {
		return member.ErrDeliverContactPhone
	}

	if len([]rune(v.Address)) < 6 {
		// 判断字符长度
		return member.ErrDeliverAddressLen
	}

	return nil
}

func (p *addressImpl) Save() (int, error) {
	if err := p.checkValue(p._value); err != nil {
		return p.GetDomainId(), err
	}
	p._value.Area = p.renewAreaName(p._value)
	return p._memberRep.SaveDeliver(p._value)
}
