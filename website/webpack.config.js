import path from 'path'
import {fileURLToPath} from 'url'
import webpack from 'webpack'
import ReactRefreshPlugin from '@pmmmwh/react-refresh-webpack-plugin'
import HtmlWebpackPlugin from 'html-webpack-plugin'

const __dirname = path.dirname(fileURLToPath(import.meta.url))

export default (env) => {
  return {
    mode: 'development',
    entry: ['./src/index.jsx'],
    output: {
      path: path.join(__dirname, 'dist'),
      filename: 'bundle.js',
      publicPath: '/',
    },
    module: {
      rules: [
        {
          test: /\.(js|jsx)$/,
          exclude: /nodeModules/,
          use: {
            loader: 'babel-loader',
          },
        },
        {
          test: /\.css$/,
          use: ['style-loader', 'css-loader'],
        },
        {
          test: /\.s[ac]ss$/i,
          use: ['style-loader', 'css-loader', 'sass-loader'],
        },
      ],
    },
    plugins: [new ReactRefreshPlugin(), new HtmlWebpackPlugin({template: './src/index.html'})],
    stats: 'minimal',
    devtool: 'inline-source-map',
    devServer: {
      // static: {
      //   directory: path.join(__dirname, 'public'),
      // },
      // webpack-dev-server 5 replaced the object form of `proxy` with an array
      // of middleware descriptors. The object form is silently ignored, so the
      // dev server would serve the app but every /api call would 404.
      proxy: [
        {
          context: ['/api'],
          target: process.env.API_URL || 'http://localhost:8080',
        },
      ],
      historyApiFallback: true,
      hot: true,
      compress: true,
      port: process.env.PORT || 3000,
    },
  }
}
