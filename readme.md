# Simple YouTube Client Uploader (Go-Lang)

provides simple console application, writen on go-lang [![Build Status](https://travis-ci.org/hawk911/youtube_api.svg?branch=master)](https://travis-ci.org/hawk911/youtube_api)

## Download

```
//TODO add the travis or drone to build binary release
```

## Invoke

* Enable YouTube API on your Google developer console
* Create OAuth Client Key
* Invoke with params

example parameters:  

### Add Video

```
-clientid="YOU clientID" -secret=" YOU secret" -filename result.mp4 -title "Обучающее видео"  -playlist тестлист -keywords "Тесты, Обучение, xDD"  -description "Описание"
```

### Delete Video

```
-clientid="YOU clientID" -secret=" YOU secret" -deleteid VIDEO_YOUTUBE_ID -playlist YOU_PlAULIST_NAME
```
