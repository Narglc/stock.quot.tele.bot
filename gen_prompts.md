curl -X POST http://127.0.0.1:5001/gen \
  -H 'Content-Type: application/json' \
  -d '{"prompt":"海边的穿泳装奔跑的少女", "negative":"不想要的", "seed":42}' \
  -o out.png

curl -X POST http://127.0.0.1:5001/gen \
  -H 'Content-Type: application/json' \
  -d '{"prompt":"室内卧室，年轻东亚美女，黑长微卷湿发，温柔淡颜红唇，白皙细腻皮肤，慵懒坐在白色毛绒大床，米白色软包床头；酒红色蕾丝透视抹胸连体衣 + 同色系蕾丝开衫外搭，半褪黑色蕾丝边丝袜，单脚裸露，体态柔媚，柔和自然光，私房写真，高清质感，浅淡氛围感，构图居中，细腻皮肤纹理，真人风格"}' \
  -o woman.png


