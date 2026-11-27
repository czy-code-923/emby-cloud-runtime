export type MemberService={id:string;name:string;enabled:boolean;library_ids:string[];lines:string[];register_enabled:boolean;renew_enabled:boolean;days:number;points_enabled:boolean;points:number};
export type MemberServices={revision:number;items:MemberService[];libraries:{id:string;name:string}[];libraries_available:boolean};
export type MyServices={items:{id:string;name:string;access:boolean;until:number;included:boolean;lines:string[];points_enabled:boolean}[]};
