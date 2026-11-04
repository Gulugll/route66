// jest 自动 mock（与 node_modules 同级的 __mocks__ 目录对 node 模块自动生效）。
// 原生模块在单测环境不存在，组件测试只关心 props 与回调。
const React = require('react');
const WebView = React.forwardRef(() => null);
WebView.injectJavaScript = () => {};
module.exports = { WebView, default: WebView };
