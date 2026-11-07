export namespace main {
	
	export class AutostartState {
	    enabled: boolean;
	    value: string;
	
	    static createFrom(source: any = {}) {
	        return new AutostartState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.value = source["value"];
	    }
	}
	export class Status {
	    state: string;
	    ssid: string;
	    ipv4: string;
	    gateway: string;
	    gateway_mac: string;
	    mac: string;
	    random_mac: boolean;
	    has_cred: boolean;
	    has_trust: boolean;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.state = source["state"];
	        this.ssid = source["ssid"];
	        this.ipv4 = source["ipv4"];
	        this.gateway = source["gateway"];
	        this.gateway_mac = source["gateway_mac"];
	        this.mac = source["mac"];
	        this.random_mac = source["random_mac"];
	        this.has_cred = source["has_cred"];
	        this.has_trust = source["has_trust"];
	        this.message = source["message"];
	    }
	}

}

