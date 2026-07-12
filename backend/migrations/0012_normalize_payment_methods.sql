-- Normalize safely identifiable payment methods to canonical lowercase values.
-- Unknown historical custom values are intentionally left unchanged.

update payments
set payment_method = case
    when lower(btrim(payment_method)) in ('wechat', 'we chat', 'wx', 'weixin')
      or btrim(payment_method) = '微信' then 'wechat'
    when lower(btrim(payment_method)) in ('alipay', 'ali pay', 'zhifubao')
      or btrim(payment_method) = '支付宝' then 'alipay'
    when lower(btrim(payment_method)) in ('bank', 'bank transfer', 'transfer')
      or btrim(payment_method) = '银行转账' then 'bank'
    when lower(btrim(payment_method)) = 'cash'
      or btrim(payment_method) = '现金' then 'cash'
    when lower(btrim(payment_method)) in ('other', 'others')
      or btrim(payment_method) = '其他' then 'other'
    else payment_method
end
where payment_method is not null
  and (
    lower(btrim(payment_method)) in ('wechat', 'we chat', 'wx', 'weixin', 'alipay', 'ali pay', 'zhifubao', 'bank', 'bank transfer', 'transfer', 'cash', 'other', 'others')
    or btrim(payment_method) in ('微信', '支付宝', '银行转账', '现金', '其他')
  );
