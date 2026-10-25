const localConfig = require('./config');

App({
  globalData: {
    webViewUrl: localConfig.webViewUrl || '',
  },
});
