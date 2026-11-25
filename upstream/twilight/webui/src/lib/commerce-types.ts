export interface ShopProduct {id:string;name:string;kind:number;days:number;price_cents:number;enabled:boolean;stock:number}
export interface ShopSettings {revision:number;enabled:boolean;instructions:string;receiving_account:string;channel:string;products:ShopProduct[]}
export interface ShopOrder {id:string;uid?:number;revision:number;product:ShopProduct;channel:string;receiving_account:string;instructions:string;status:string;created_at:number;pay_before:number;note?:string;admin_note?:string;code?:string;redeemed?:boolean;code_expires_at?:number}
export interface ShopCatalog {enabled:boolean;revision:number;products:ShopProduct[]}
export interface ShopOrders {username:string;items:ShopOrder[];page:number;total:number;has_next:boolean}
export interface AdminShop {settings:ShopSettings;orders:ShopOrder[];page:number;total:number;has_next:boolean}
