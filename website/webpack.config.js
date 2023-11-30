import path from 'path'
import {fileURLToPath} from 'url';
import webpack from 'webpack'
import ReactRefreshPlugin from '@pmmmwh/react-refresh-webpack-plugin'
import HtmlWebpackPlugin from 'html-webpack-plugin'

const __dirname = path.dirname(fileURLToPath(import.meta.url))

export default (env) => {

  return {
    mode: 'development',
    entry: ['./src/index.js'],
    output: {
      path: path.join(__dirname, 'dist'),
      filename: 'bundle.js',
      publicPath: '/'
    },
    module: {
      rules: [
        {
          test: /\.(js|jsx)$/,
          exclude: /nodeModules/,
          use: {
            loader: 'babel-loader'
          }
        },
        {
          test: /\.css$/,
          use: ['style-loader', 'css-loader']
        },
        {
          test: /\.s[ac]ss$/i,
          use: ['style-loader', 'css-loader', 'sass-loader'],
        },
      ]
    },
    plugins: [
      new ReactRefreshPlugin(),
      new HtmlWebpackPlugin({ template: './src/index.html' }),
    ],
    stats: 'minimal',
    devtool : 'inline-source-map',
    devServer: {
      // static: {
      //   directory: path.join(__dirname, 'public'),
      // },
      proxy: {
        '/api': (process.env.API_URL || 'http://localhost:8080'),
      },
      historyApiFallback: true,
      hot: true,
      compress: true,
      port: (process.env.PORT || 3000),
    }
  }
}
