
class TgClient {
    constructor(data) {
        this.chatId = 0;
        this.clientName = "";
        this.clientId = ""
        this.clientEmail = "";
        this.clientUid = "";
        this.approved = false;
        if (data == null) {
            return;
        }
        ObjectUtil.cloneProps(this, data);
    }

}

class TgClientMsg {
    constructor(data) {
        this.chatId = 0;
        this.msg = "";
        if (data == null) {
            return;
        }
        ObjectUtil.cloneProps(this, data);
    }

}