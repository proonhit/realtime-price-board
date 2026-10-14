const priceFmt = new Intl.NumberFormat("vi-VN", { minimumFractionDigits: 2, maximumFractionDigits: 2 });
const timeFmt = new Intl.DateTimeFormat("vi-VN", { hour: "2-digit", minute: "2-digit", second: "2-digit" });

export const formatPrice = (n: number) => priceFmt.format(n);
export const formatTime = (ms: number) => timeFmt.format(ms);
