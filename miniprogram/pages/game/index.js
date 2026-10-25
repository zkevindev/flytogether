const app = getApp();

Page({
  data: {
    url: '',
    issue: '',
  },

  onLoad() {
    const candidate = app.globalData.webViewUrl;
    if (typeof candidate !== 'string' || !candidate.trim()) {
      this.setData({ issue: '还没有配置体验地址。请按内测说明创建本机配置文件。' });
      return;
    }

    const url = candidate.trim();
    if (!/^https:\/\/[a-z0-9.-]+(?::\d+)?(?:[/?#][^\s]*)?$/i.test(url)) {
      this.setData({ issue: '体验地址必须是有效的 HTTPS 网页地址。' });
      return;
    }

    this.setData({ url });
  },

  onWebViewError(event) {
    console.error('Mini Program web-view failed to load', event.detail);
    this.setData({ issue: '网页暂时无法打开。请检查小程序后台业务域名和 HTTPS 配置。' });
  },
});
