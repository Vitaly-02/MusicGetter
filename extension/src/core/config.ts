declare const __BACKEND_ORIGIN__: string;
export const backendOrigin = __BACKEND_ORIGIN__;
const url = new URL(backendOrigin);
export const backendPermission = `${url.protocol}//${url.hostname}/*`;
