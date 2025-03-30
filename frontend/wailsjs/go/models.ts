export namespace main {
	
	export class SocksRule {
	    id: string;
	    name: string;
	    port: number;
	    username: string;
	    password: string;
	    noAuth: boolean;
	    running: boolean;
	    enableUDP: boolean;
	    uploadBytes: number;
	    downloadBytes: number;
	
	    static createFrom(source: any = {}) {
	        return new SocksRule(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.port = source["port"];
	        this.username = source["username"];
	        this.password = source["password"];
	        this.noAuth = source["noAuth"];
	        this.running = source["running"];
	        this.enableUDP = source["enableUDP"];
	        this.uploadBytes = source["uploadBytes"];
	        this.downloadBytes = source["downloadBytes"];
	    }
	}

}

