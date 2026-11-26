export interface PaymentChannel {id:string;name:string;kind:string;enabled:boolean;account:string;instructions:string;qr_url:string;version:number}
export interface ShopProduct {id:string;name:string;kind:number;days:number;price_cents:number;enabled:boolean;stock:number}
export interface ShopSettings {channels?:PaymentChannel[];default_channel?:string;donation_qr?:string;revision:number;enabled:boolean;instructions:string;receiving_account:string;channel:string;products:ShopProduct[]}
export interface ShopOrder {refund_preview?:{proven:boolean;current_expiry:number;remaining_seconds:number;result_expiry?:number;hint:string};payment_secret_present?:boolean;id:string;payment_qr?:string;payment_kind?:string;points_cost?:number;uid?:number;revision:number;product:ShopProduct;channel:string;receiving_account:string;instructions:string;status:string;created_at:number;pay_before:number;note?:string;admin_note?:string;code?:string;redeemed?:boolean;code_expires_at?:number}
export interface ShopCatalog {channels?:PaymentChannel[];default_channel?:string;donation_qr?:string;enabled:boolean;revision:number;products:ShopProduct[]}
export interface ShopOrders {username:string;items:ShopOrder[];page:number;total:number;has_next:boolean}
export interface AdminShop {settings:ShopSettings;orders:ShopOrder[];page:number;total:number;has_next:boolean}
